package pinger

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	pkgraw "example.com/rbmq-demo/pkg/raw"
	pkgthrottle "example.com/rbmq-demo/pkg/throttle"
	pkgutils "example.com/rbmq-demo/pkg/utils"
)

type SimplePinger struct {
	pingRequest *SimplePingRequest
	proxyHub    *pkgthrottle.SharedThrottleHub
}

type SimplePingerConfig struct {
	PingRequest *SimplePingRequest
	ProxyHub    *pkgthrottle.SharedThrottleHub
}

func NewSimplePinger(cfg SimplePingerConfig) *SimplePinger {
	sp := new(SimplePinger)
	sp.pingRequest = cfg.PingRequest
	sp.proxyHub = cfg.ProxyHub
	return sp
}

func (sp *SimplePinger) Ping(ctx context.Context) <-chan PingEvent {
	outputEVChan := make(chan PingEvent)
	go func() {
		defer close(outputEVChan)

		ph := sp.proxyHub
		pingRequest := sp.pingRequest
		var err error

		buffRedundancyFactor := 2
		pkgTimeout := time.Duration(sp.pingRequest.PktTimeoutMilliseconds) * time.Millisecond
		pkgInterval := time.Duration(sp.pingRequest.IntvMilliseconds) * time.Millisecond
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
		if sp.pingRequest.ResolveTimeoutMilliseconds != nil {
			resolveTimeout = time.Duration(*sp.pingRequest.ResolveTimeoutMilliseconds) * time.Millisecond
		}
		var resolver *net.Resolver = net.DefaultResolver
		if pingRequest.Resolver != nil {
			resolver = &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
					d := net.Dialer{
						Timeout: resolveTimeout,
					}
					return d.DialContext(ctx, network, *pingRequest.Resolver)
				},
			}
		}

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
		dstPtr, err := pkgutils.SelectDstIP(ctx, resolver, destHostName, pingRequest.PreferV4, pingRequest.PreferV6)
		if err != nil {
			outputEVChan <- PingEvent{Error: err}
			return
		}

		if dstPtr == nil {
			outputEVChan <- PingEvent{Error: fmt.Errorf("no destination IP found")}
			return
		}
		dst := *dstPtr

		icmpId := rand.Intn(0x10000)

		var transceiver pkgraw.GeneralICMPTransceiver
		if dst.IP.To4() != nil {
			icmp4tr, err := pkgraw.NewICMP4Transceiver(pkgraw.ICMP4TransceiverConfig{
				ID: icmpId,
			})
			if err != nil {
				log.Fatalf("failed to create ICMP4 transceiver: %v", err)
			}
			if err := icmp4tr.Run(ctx); err != nil {
				log.Fatalf("failed to run ICMP4 transceiver: %v", err)
			}
			transceiver = icmp4tr
		} else {
			icmp6tr, err := pkgraw.NewICMP6Transceiver(pkgraw.ICMP6TransceiverConfig{
				ID: icmpId,
			})
			if err != nil {
				log.Fatalf("failed to create ICMP6 transceiver: %v", err)
			}
			if err := icmp6tr.Run(ctx); err != nil {
				log.Fatalf("failed to run ICMP6 transceiver: %v", err)
			}
			transceiver = icmp6tr
		}

		throttleProxySrc := make(chan interface{})
		proxyCh, err := ph.CreateProxy(ctx, throttleProxySrc)
		if err != nil {
			log.Fatalf("failed to create proxy: %v", err)
		}

		var pkgWg sync.WaitGroup
		defer func() {
			if pingRequest.TotalPkts != nil {

				pkgWg.Wait()

			}
		}()

		go func() {
			for ev := range tracker.RecvEvC {
				var wrappedEV *pkgraw.ICMPTrackerEntry = &ev
				wrappedEV, _ = ev.ResolveRDNS(ctx, resolver)
				outputEVChan <- PingEvent{Data: wrappedEV}

				if pingRequest.TotalPkts != nil {

					pkgWg.Done()
					if ev.Seq >= *pingRequest.TotalPkts {

						return
					}
				}
			}
		}()

		go func() {
			receiverCh := transceiver.GetReceiver()
			for {
				subCh := make(chan pkgraw.ICMPReceiveReply)
				select {
				case <-ctx.Done():
					return
				case receiverCh <- subCh:
					reply := <-subCh
					tracker.MarkReceived(reply.Seq, reply)
				}
			}
		}()

		senderCh := transceiver.GetSender()

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case reqraw := <-proxyCh:
					req, ok := reqraw.(pkgraw.ICMPSendRequest)
					if !ok {
						log.Fatal("wrong format")
					}
					senderCh <- req

					tracker.MarkSent(req.Seq, req.TTL)
				}
			}
		}()

		numPktsSent := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
				req := pkgraw.ICMPSendRequest{
					Seq: numPktsSent + 1,
					TTL: pingRequest.TTL[numPktsSent%len(pingRequest.TTL)],
					Dst: dst,
				}
				throttleProxySrc <- req

				numPktsSent++
				if pingRequest.TotalPkts != nil {
					pkgWg.Add(1)
					if numPktsSent >= *pingRequest.TotalPkts {
						return
					}
				}
				<-time.After(time.Duration(pingRequest.IntvMilliseconds) * time.Millisecond)
			}
		}

	}()
	return outputEVChan
}
