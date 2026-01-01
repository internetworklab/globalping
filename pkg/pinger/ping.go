package pinger

import (
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"strings"
	"time"

	cryptoRand "crypto/rand"

	pkgipinfo "example.com/rbmq-demo/pkg/ipinfo"
	pkgraw "example.com/rbmq-demo/pkg/raw"
	pkgutils "example.com/rbmq-demo/pkg/utils"

	pkgmyprom "example.com/rbmq-demo/pkg/myprom"
	"github.com/prometheus/client_golang/prometheus"
)

type SimplePinger struct {
	PingRequest   *SimplePingRequest
	IPInfoAdapter pkgipinfo.GeneralIPInfoAdapter
	RespondRange  []net.IPNet
}

func (sp *SimplePinger) Ping(ctx context.Context) <-chan PingEvent {
	commonLabels := ctx.Value(pkgutils.CtxKeyPromCommonLabels).(prometheus.Labels)
	if commonLabels == nil {
		panic("failed to obtain common labels from context")
	}
	counterStore := ctx.Value(pkgutils.CtxKeyPrometheusCounterStore).(*pkgmyprom.CounterStore)
	if counterStore == nil {
		panic("failed to obtain counter store from context")
	}

	outputEVChan := make(chan PingEvent)
	go func() {
		defer close(outputEVChan)

		pingRequest := sp.PingRequest
		var err error

		buffRedundancyFactor := 2
		pkgTimeout := time.Duration(sp.PingRequest.PktTimeoutMilliseconds) * time.Millisecond
		pkgInterval := time.Duration(sp.PingRequest.IntvMilliseconds) * time.Millisecond
		trackerConfig := &pkgraw.ICMPTrackerConfig{
			PacketTimeout:                 pkgTimeout,
			TimeoutChannelEventBufferSize: buffRedundancyFactor * int(pkgTimeout.Seconds()/math.Max(1, pkgInterval.Seconds())),
		}
		tracker, err := pkgraw.NewICMPTracker(trackerConfig)
		if err != nil {
			log.Fatalf("failed to create ICMP tracker: %v", err)
		}
		tracker.Run(ctx)

		resolveTimeout := 10 * time.Second
		if sp.PingRequest.ResolveTimeoutMilliseconds != nil {
			resolveTimeout = time.Duration(*sp.PingRequest.ResolveTimeoutMilliseconds) * time.Millisecond
		}
		resolver := pkgutils.NewCustomResolver(pingRequest.Resolver, resolveTimeout)

		destHostName := pingRequest.Destination
		if destHostName == "" {
			if len(pingRequest.Targets) == 0 {
				outputEVChan <- PingEvent{Error: fmt.Errorf("destination or targets are required")}
				return
			}
			destHostName = strings.TrimSpace(pingRequest.Targets[0])
		}
		if destHostName == "" {
			outputEVChan <- PingEvent{Error: fmt.Errorf("target is empty")}
			return
		}

		dstPtr, err := pkgutils.SelectDstIP(ctx, resolver, destHostName, pingRequest.PreferV4, pingRequest.PreferV6, sp.RespondRange)
		if err != nil {
			outputEVChan <- PingEvent{Error: err}
			return
		}

		if dstPtr == nil {
			outputEVChan <- PingEvent{Error: fmt.Errorf("no destination IP found")}
			return
		}
		dst := *dstPtr

		useUDP := sp.PingRequest.L3PacketType != nil && *sp.PingRequest.L3PacketType == "udp"
		udpPort := sp.PingRequest.UDPDstPort

		var transceiver pkgraw.GeneralICMPTransceiver
		var transceiverErrCh <-chan error
		if dst.IP.To4() != nil {
			icmp4tr, err := pkgraw.NewICMP4Transceiver(pkgraw.ICMP4TransceiverConfig{
				UDPBasePort: udpPort,
				UseUDP:      useUDP,
			})
			if err != nil {
				log.Fatalf("failed to create ICMP4 transceiver: %v", err)
			}
			transceiverErrCh = icmp4tr.Run(ctx)
			transceiver = icmp4tr
		} else {
			icmp6tr, err := pkgraw.NewICMP6Transceiver(pkgraw.ICMP6TransceiverConfig{
				UseUDP:      useUDP,
				UDPBasePort: udpPort,
			})
			if err != nil {
				log.Fatalf("failed to create ICMP6 transceiver: %v", err)
			}
			transceiverErrCh = icmp6tr.Run(ctx)
			transceiver = icmp6tr
		}

		payloadLen := 0
		if pingRequest.RandomPayloadSize != nil && *pingRequest.RandomPayloadSize > 0 {
			payloadLen = *pingRequest.RandomPayloadSize
		}
		payloadLen = int(math.Max(0, math.Min(float64(pkgutils.GetMinimumMTU()), float64(payloadLen))))
		payload := make([]byte, payloadLen)
		if len(payload) > 0 {
			cryptoRand.Read(payload)
		}

		type SendControl struct {
			PMTU *int
			TTL  int
		}

		ctrlSignals := make(chan SendControl, 1)
		ctrlSignals <- SendControl{
			PMTU: nil,
			TTL:  pingRequest.TTL.GetNext(),
		}

		waitForEVGenCh := make(chan interface{})
		go func() {
			log.Printf("ICMP Event-generating goroutine for %s is started", dst.String())
			defer close(waitForEVGenCh)
			defer close(ctrlSignals)
			defer log.Printf("ICMP Event-generating goroutine for %s is exitting", dst.String())

			for {
				select {
				case <-ctx.Done():
					log.Printf("In ICMP Event-generating goroutine for %s, got context done", dst.String())
					return
				case ev, ok := <-tracker.RecvEvC:
					if !ok {
						// means that the tracker is no longer usable
						log.Printf("the ICMP event tracker is confirmed to be closed")
						return
					}

					var wrappedEV *pkgraw.ICMPTrackerEntry = &ev

					if wrappedEV.FoundLastHop() {
						if autoTTL, ok := pingRequest.TTL.(*AutoTTL); ok {
							autoTTL.Reset()
						}
					}

					if sp.IPInfoAdapter != nil {
						wrappedEV, err = wrappedEV.ResolveIPInfo(ctx, sp.IPInfoAdapter)
						if err != nil {
							log.Printf("failed to resolve IP info: %v", err)
							err = nil
						}
					}

					wrappedEV, err = wrappedEV.ResolveRDNS(ctx, resolver)
					if err != nil {
						log.Printf("failed to resolve RDNS: %v", err)
						err = nil
					}

					outputEVChan <- PingEvent{Data: wrappedEV}

					if pingRequest.TotalPkts != nil && tracker.GetUnAcked() == 0 && tracker.GetAckedSeq() == *pingRequest.TotalPkts {
						// the SEQ of reply packet is un-reliable, since the order of reply packets is not guaranteed.
						log.Printf("Max number of packets to send: %d, received ev of seq %d, no more icmp events will be generated", *pingRequest.TotalPkts, ev.Seq)
						return
					}

					ctrlSignals <- SendControl{
						PMTU: wrappedEV.GetPMTU(),
						TTL:  pingRequest.TTL.GetNext(),
					}
				}
			}
		}()

		go func() {
			log.Printf("ICMPReceiving goroutine for %s is started", dst.String())
			defer log.Printf("ICMPReceiving goroutine for %s is exitting", dst.String())
			defer tracker.ForgetAllAndClose()

			for reply := range transceiver.GetReceiver() {
				if err := tracker.MarkReceived(reply.Seq, reply); err != nil {
					log.Printf("In ICMPReceiving goroutine for %s, failed to mark received: %v", dst.String(), err)
					return
				}
				counterStore.NumPktsReceived.With(commonLabels).Add(1.0)
			}
		}()

		go func() {
			log.Printf("ICMPSending goroutine for %s is started", dst.String())
			defer log.Printf("ICMPSending goroutine for %s is exitting", dst.String())
			defer transceiver.Close()

			numPktsSent := 0
			for {
				select {
				case <-ctx.Done():
					log.Printf("In ICMPSending goroutine for %s, got context done", dst.String())
					return
				case err := <-transceiverErrCh:
					log.Printf("In ICMPSending goroutine for %s, got transceiver error: %v", dst.String(), err)
					return
				case ctrlSignal, ok := <-ctrlSignals:
					if !ok {
						log.Printf("In ICMPSending goroutine for %s, no more TTL values will be generated", dst.String())
						return
					}

					req := pkgraw.ICMPSendRequest{
						Seq:  numPktsSent + 1,
						TTL:  ctrlSignal.TTL,
						Dst:  dst,
						Data: payload,
						PMTU: ctrlSignal.PMTU,
					}

					senderCh, ok := <-transceiver.GetSender()
					if !ok {
						// the transceiver no longer accepts new requests
						log.Printf("In ICMPSending goroutine for %s, transceiver no longer accepts new requests", dst.String())
						return
					}

					// MarkSent first, then actually send it.
					// otherwise, if send it before marking sent, and if the reply is received too early,
					// there would be a race condition (the reply packet can't find the corresponding sent entry)
					if err := tracker.MarkSent(req.Seq, req.TTL); err != nil {
						log.Printf("In ICMPSending goroutine for %s, failed to mark sent: %v", dst.String(), err)
						return
					}
					senderCh <- req

					counterStore.NumPktsSent.With(commonLabels).Add(1.0)

					numPktsSent++
					if pingRequest.TotalPkts != nil {
						if numPktsSent >= *pingRequest.TotalPkts {
							log.Printf("In ICMPSending goroutine for %s, no more packets to send: %d", dst.String(), *pingRequest.TotalPkts)
							return
						}
					}
					<-time.After(time.Duration(pingRequest.IntvMilliseconds) * time.Millisecond)
				}
			}
		}()

		<-waitForEVGenCh
	}()

	return outputEVChan
}
