package pinger

import (
	"context"
	cryptoRand "crypto/rand"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	pkgipinfo "example.com/rbmq-demo/pkg/ipinfo"
	pkgraw "example.com/rbmq-demo/pkg/raw"
	pkgutils "example.com/rbmq-demo/pkg/utils"
)

type SimplePinger struct {
	pingRequest   *SimplePingRequest
	ipinfoAdapter pkgipinfo.GeneralIPInfoAdapter
}

type SimplePingerConfig struct {
	PingRequest   *SimplePingRequest
	IPInfoAdapter pkgipinfo.GeneralIPInfoAdapter
}

func NewSimplePinger(cfg SimplePingerConfig) *SimplePinger {
	sp := new(SimplePinger)
	sp.pingRequest = cfg.PingRequest
	sp.ipinfoAdapter = cfg.IPInfoAdapter
	return sp
}

func (sp *SimplePinger) Ping(ctx context.Context) <-chan PingEvent {
	outputEVChan := make(chan PingEvent)
	go func() {
		defer close(outputEVChan)

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

		var pkgWg sync.WaitGroup
		defer func() {
			if pingRequest.TotalPkts != nil {

				pkgWg.Wait()

			}
		}()

		var payloadManager *PayloadManager
		if pingRequest.RandomPayloadSize != nil {
			payloadManager = NewPayloadManager(*pingRequest.RandomPayloadSize)
		}

		ttlCh := make(chan int, 1)
		ttlCh <- pingRequest.TTL.GetNext()

		go func() {
			for ev := range tracker.RecvEvC {
				var wrappedEV *pkgraw.ICMPTrackerEntry = &ev
				if dst.IP != nil {
					var foundLastHop bool
					wrappedEV, foundLastHop = wrappedEV.MarkLastHop(dst)
					if foundLastHop {
						if autoTTL, ok := pingRequest.TTL.(*AutoTTL); ok {
							log.Printf("[DBG] resetting auto TTL")
							autoTTL.Reset()
						}
					}
				}
				nextTTL := pingRequest.TTL.GetNext()
				log.Printf("[DBG] next TTL=%d", nextTTL)
				ttlCh <- nextTTL

				if sp.ipinfoAdapter != nil {
					wrappedEV, err = wrappedEV.ResolveIPInfo(ctx, sp.ipinfoAdapter)
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

				if payloadManager != nil {
					for _, icmpReply := range wrappedEV.Raw {
						if icmpReply.SetMTUTo != nil && icmpReply.ShrinkICMPPayloadTo != nil {
							log.Printf("[DBG] Shrinking payload due to PMTU msg: SetMTUTo=%d, ShrinkICMPPayloadTo=%d, dst=%s", *icmpReply.SetMTUTo, *icmpReply.ShrinkICMPPayloadTo, dst.String())
							payloadManager.Shrink(icmpReply.ShrinkICMPPayloadTo)
						}
					}
				}

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

		numPktsSent := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
				ttl := <-ttlCh

				req := pkgraw.ICMPSendRequest{
					Seq: numPktsSent + 1,
					TTL: ttl,
					Dst: dst,
				}

				if payloadManager != nil {
					req.Data = payloadManager.GetPayload()
				}

				senderCh <- req

				tracker.MarkSent(req.Seq, req.TTL)

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

type PayloadManager struct {
	data []byte
	lock sync.Mutex
}

func NewPayloadManager(size int) *PayloadManager {
	pm := &PayloadManager{
		lock: sync.Mutex{},
	}
	pm.data = make([]byte, size)
	cryptoRand.Read(pm.data)

	return pm
}

func (pm *PayloadManager) GetPayload() []byte {
	pm.lock.Lock()
	defer pm.lock.Unlock()

	return pm.data
}

func (pm *PayloadManager) Shrink(shrinkTo *int) {
	pm.lock.Lock()
	defer pm.lock.Unlock()

	if shrinkTo == nil {
		return
	}
	originalSize := len(pm.data)
	if *shrinkTo >= originalSize {
		return
	}
	pm.data = pm.data[:*shrinkTo]
	log.Printf("[DBG] Shrunk payload from %d to %d bytes, shrinkTo=%d", originalSize, *shrinkTo, *shrinkTo)
}
