package pinger

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	pkgipinfo "example.com/rbmq-demo/pkg/ipinfo"
	pkgtcping "example.com/rbmq-demo/pkg/tcping"
	pkgutils "example.com/rbmq-demo/pkg/utils"
)

type TCPSYNPinger struct {
	PingRequest   *SimplePingRequest
	OnSent        pkgtcping.TCPSYNSenderHook
	OnReceived    pkgtcping.TCPSYNSenderHook
	RespondRange  []net.IPNet
	IPInfoAdapter pkgipinfo.GeneralIPInfoAdapter
}

func (pinger *TCPSYNPinger) getHostAndPort(ctx context.Context) (net.IP, int, error) {
	destination := pinger.PingRequest.Destination
	if destination == "" {
		if len(pinger.PingRequest.Targets) > 0 {
			destination = pinger.PingRequest.Targets[0]
		}
	}
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return nil, 0, fmt.Errorf("destination is required")
	}

	host, port, err := net.SplitHostPort(destination)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to split host and port from destination %s: %v", destination, err)
	}

	resolver := pkgutils.NewCustomResolver(pinger.PingRequest.Resolver, 10*time.Second)
	dstIPAddr, err := pkgutils.SelectDstIP(ctx, resolver, host, pinger.PingRequest.PreferV4, pinger.PingRequest.PreferV6, pinger.RespondRange)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to select dst ip: %v", err)
	}

	if dstIPAddr == nil {
		return nil, 0, fmt.Errorf("no dst ip available for %s", host)
	}

	dstIP := dstIPAddr.IP
	dstPort, err := strconv.Atoi(port)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to convert port to int: %v", err)
	}
	return dstIP, dstPort, nil
}

func postProcessReceivedPkt(ctx context.Context, receivedPkt *pkgtcping.PacketInfo, resolver *net.Resolver, ipinfoAdapter pkgipinfo.GeneralIPInfoAdapter) (*pkgtcping.PacketInfo, error) {
	if receivedPkt == nil {
		return nil, nil
	}

	var err error
	receivedPkt, err = receivedPkt.ResolveRDNS(ctx, resolver)
	if err != nil {
		if _, ok := err.(*net.DNSError); !ok {
			return receivedPkt, fmt.Errorf("failed to resolve rdns for %s: %v", receivedPkt.SrcIP.String(), err)
		}
	}

	if ipinfoAdapter != nil {
		receivedPkt, err = receivedPkt.ResolveIPInfo(ctx, ipinfoAdapter)
		if err != nil {
			return receivedPkt, fmt.Errorf("failed to resolve ip info for %s: %v", receivedPkt.SrcIP.String(), err)
		}
	}

	return receivedPkt, nil
}

func (pinger *TCPSYNPinger) Ping(ctx context.Context) <-chan PingEvent {
	// pre-allocate 1 slot for error reporting, so that it can exit once there is an error
	evCh := make(chan PingEvent, 1)
	go func() {
		defer close(evCh)

		dstIP, dstPort, err := pinger.getHostAndPort(ctx)
		if err != nil {
			evCh <- PingEvent{Error: fmt.Errorf("failed to get host and port: %v", err)}
			return
		}

		var sender pkgtcping.Sender
		senderConfig := &pkgtcping.TCPSYNSenderConfig{
			OnSent: pinger.OnSent,
		}
		if dstIP.To4() == nil {
			sender6, err := pkgtcping.NewTCPSYNSender6(ctx, senderConfig)
			if err != nil {
				evCh <- PingEvent{Error: fmt.Errorf("failed to create ipv6 tcp syn sender: %v", err)}
				return
			}
			defer sender6.Close()
			sender = sender6
		} else {
			sender4, err := pkgtcping.NewTCPSYNSender(ctx, senderConfig)
			if err != nil {
				evCh <- PingEvent{Error: fmt.Errorf("failed to create ipv4 tcp syn sender: %v", err)}
				return
			}
			defer sender4.Close()
			sender = sender4
		}

		trackerConfig := &pkgtcping.TrackerConfig{}
		tracker := pkgtcping.NewTracker(trackerConfig)
		tracker.Run(ctx)

		resolver := pkgutils.NewCustomResolver(pinger.PingRequest.Resolver, 10*time.Second)

		intvMs := pinger.PingRequest.IntvMilliseconds
		ticker := time.NewTicker(time.Duration(intvMs) * time.Millisecond)

		pktTimeoutMs := pinger.PingRequest.PktTimeoutMilliseconds
		pktTimeout := time.Duration(pktTimeoutMs) * time.Millisecond

		defer ticker.Stop()
		allConfirmedCh := make(chan interface{})
		defer func() {
			log.Printf("Waiting for all un-acked tcp syn packets to be acked")
			<-allConfirmedCh
			log.Printf("All un-acked tcp syn packets are acked")
		}()

		go func() {
			defer log.Printf("Exiting tcp syn filtering goroutine")

			requireSYN := true
			requireACK := true
			filteredCh := pkgtcping.FilterPackets(sender.GetPackets(), &pkgtcping.FilterRequirements{
				SYN:     &requireSYN,
				ACK:     &requireACK,
				SrcPort: &dstPort,
			})
			for {
				select {
				case <-ctx.Done():
					return
				case pkgInfo, ok := <-filteredCh:
					if !ok {
						return
					}
					tracker.MarkReceived(pkgInfo)
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-tracker.EventC:
				if !ok {
					return
				}
				if event.Type != pkgtcping.TrackerEVReceived && event.Type != pkgtcping.TrackerEVTimeout {
					log.Printf("received unexpected event type: %v", event.Type)
					continue
				}

				if event.Type == pkgtcping.TrackerEVReceived && event.Details != nil {
					var err error
					event.Details.ReceivedPkt, err = postProcessReceivedPkt(ctx, event.Details.ReceivedPkt, resolver, pinger.IPInfoAdapter)
					if err != nil {
						log.Printf("failed to post process received pkt: %v", err)
					}

					if receivedPkt := event.Details.ReceivedPkt; receivedPkt != nil && pinger.OnReceived != nil {
						pinger.OnReceived(
							ctx,
							receivedPkt.SrcIP,
							int(receivedPkt.TCP.SrcPort),
							receivedPkt.DstIP,
							int(receivedPkt.TCP.DstPort),
							receivedPkt.Size,
						)
					}
				}

				evCh <- PingEvent{Data: event}

				if totalPkts := pinger.PingRequest.TotalPkts; totalPkts != nil {
					if *totalPkts == event.Details.Seq+1 {
						close(allConfirmedCh)
						return
					}
				}
			case <-ticker.C:
				initSeqNum := rand.Uint32()
				synRequest := &pkgtcping.TCPSYNRequest{
					DstIP:   dstIP,
					DstPort: dstPort,
					Timeout: pktTimeout,
					Seq:     initSeqNum,
					Ack:     0,
					Window:  0xffff,
				}
				receipt, err := sender.Send(ctx, synRequest, tracker)
				if err != nil {
					evCh <- PingEvent{Error: fmt.Errorf("failed to send tcp syn: %v", err)}
					return
				}

				if totalPkts := pinger.PingRequest.TotalPkts; totalPkts != nil {
					if receipt.Seq+1 == *totalPkts {
						log.Printf("No more tcp syn packets to send")
						ticker.Stop()
					}
				}
			}
		}

	}()
	return evCh
}
