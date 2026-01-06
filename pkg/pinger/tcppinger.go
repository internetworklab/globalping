package pinger

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	pkgtcping "example.com/rbmq-demo/pkg/tcping"
	pkgutils "example.com/rbmq-demo/pkg/utils"
)

type TCPSYNPinger struct {
	PingRequest  *SimplePingRequest
	OnSent       pkgtcping.TCPSYNSenderHook
	OnReceived   pkgtcping.TCPSYNSenderHook
	RespondRange []net.IPNet
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

func (pinger *TCPSYNPinger) Ping(ctx context.Context) <-chan PingEvent {
	// pre-allocate 1 slot for error reporting, so that it can exit once there is an error
	evCh := make(chan PingEvent, 1)
	go func() {
		defer close(evCh)

		ctx := context.Background()

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

		allConfirmedCh := make(chan bool, 1)
		go func(ctx context.Context) {
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-tracker.EventC:
					if !ok {
						return
					}

					evCh <- PingEvent{Data: event}
					if event.Type == pkgtcping.TrackerEVReceived || event.Type == pkgtcping.TrackerEVTimeout {
						if event.Type == pkgtcping.TrackerEVReceived && pinger.OnReceived != nil {
							receivedPkt := event.Entry.Value.ReceivedPkt
							pinger.OnReceived(
								ctx,
								receivedPkt.SrcIP,
								int(receivedPkt.TCP.SrcPort),
								receivedPkt.DstIP,
								int(receivedPkt.TCP.DstPort),
								receivedPkt.Size,
							)
						}

						if totalPkts := pinger.PingRequest.TotalPkts; totalPkts != nil {
							if *totalPkts == event.Entry.Value.Seq+1 {
								allConfirmedCh <- true
								return
							}
						}

					}
				}
			}
		}(ctx)

		go func(ctx context.Context) {
			rbCh := sender.GetPackets()
			requireSYN := true
			requireACK := true
			filteredCh := pkgtcping.FilterPackets(rbCh, &pkgtcping.FilterRequirements{
				SYN:     &requireSYN,
				ACK:     &requireACK,
				SrcPort: &dstPort,
			})

			for {
				select {
				case <-ctx.Done():
					return
				case pktInfo, ok := <-filteredCh:
					if !ok {
						return
					}
					tracker.MarkReceived(pktInfo)
				}
			}
		}(ctx)

		intvMs := pinger.PingRequest.IntvMilliseconds
		ticker := time.NewTicker(time.Duration(intvMs) * time.Millisecond)

		pktTimeoutMs := pinger.PingRequest.PktTimeoutMilliseconds
		pktTimeout := time.Duration(pktTimeoutMs) * time.Millisecond

		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
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
						// no more packets to send
						<-allConfirmedCh
						return
					}
				}
			}
		}

	}()
	return evCh
}
