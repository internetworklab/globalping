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

		icmpId := 1024 + rand.Intn(0x10000-1024)
		useUDP := sp.PingRequest.L3PacketType != nil && *sp.PingRequest.L3PacketType == "udp"
		udpPort := sp.PingRequest.UDPDstPort

		var transceiver pkgraw.GeneralICMPTransceiver
		var transceiverErrCh <-chan error
		if dst.IP.To4() != nil {
			icmp4tr, err := pkgraw.NewICMP4Transceiver(pkgraw.ICMP4TransceiverConfig{
				ID:          icmpId,
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
				UseUDP: useUDP,
			})
			if err != nil {
				log.Fatalf("failed to create ICMP6 transceiver: %v", err)
			}
			transceiverErrCh = icmp6tr.Run(ctx)
			transceiver = icmp6tr
		}

		var payloadManager *PayloadManager
		if pingRequest.RandomPayloadSize != nil {
			payloadManager = NewPayloadManager(*pingRequest.RandomPayloadSize)
		}

		ttlCh := make(chan int, 1)
		ttlCh <- pingRequest.TTL.GetNext()

		waitForICMPEVGenGoroutine := sync.WaitGroup{}

		waitForICMPEVGenGoroutine.Add(1)
		go func() {
			log.Printf("ICMP Event-generating goroutine for %s is started", dst.String())
			defer waitForICMPEVGenGoroutine.Done()
			defer close(ttlCh)
			defer log.Printf("ICMP Event-generating goroutine for %s is exitting", dst.String())

			for {
				select {
				case <-ctx.Done():
					log.Printf("In ICMP Event-generating goroutine for %s, got context done", dst.String())
					return
				case ev := <-tracker.RecvEvC:
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

					if payloadManager != nil {
						for _, icmpReply := range wrappedEV.Raw {
							if icmpReply.SetMTUTo != nil && icmpReply.ShrinkICMPPayloadTo != nil {
								log.Printf("[DBG] Shrinking payload due to PMTU msg: SetMTUTo=%d, ShrinkICMPPayloadTo=%d, dst=%s", *icmpReply.SetMTUTo, *icmpReply.ShrinkICMPPayloadTo, dst.String())
								payloadManager.Shrink(icmpReply.ShrinkICMPPayloadTo)
							}
						}
					}

					outputEVChan <- PingEvent{Data: wrappedEV}

					if pingRequest.TotalPkts != nil && tracker.GetUnAcked() == 0 && tracker.GetAckedSeq() == *pingRequest.TotalPkts {
						// the SEQ of reply packet is un-reliable, since the order of reply packets is not guaranteed.
						log.Printf("Max number of packets to send: %d, received ev of seq %d, no more icmp events will be generated", *pingRequest.TotalPkts, ev.Seq)
						return
					}

					ttlCh <- pingRequest.TTL.GetNext()
				}

			}

		}()

		receivingCtx, cancelReceiving := context.WithCancel(context.Background())
		defer cancelReceiving()

		go func() {
			log.Printf("ICMPReceiving goroutine for %s is started", dst.String())
			defer log.Printf("ICMPReceiving goroutine for %s is exitting", dst.String())

			receiverCh := transceiver.GetReceiver()
			for {
				subCh := make(chan pkgraw.ICMPReceiveReply)
				select {
				case <-receivingCtx.Done():
					return
				case receiverCh <- subCh:
					reply := <-subCh
					tracker.MarkReceived(reply.Seq, reply)
					counterStore.NumPktsReceived.With(commonLabels).Add(1.0)
				}
			}
		}()

		go func() {
			log.Printf("ICMPSending goroutine for %s is started", dst.String())
			defer log.Printf("ICMPSending goroutine for %s is exitting", dst.String())

			senderCh := transceiver.GetSender()
			numPktsSent := 0
			for {
				select {
				case <-ctx.Done():
					log.Printf("In ICMPSending goroutine for %s, got context done", dst.String())
					return
				case err := <-transceiverErrCh:
					log.Printf("In ICMPSending goroutine for %s, got transceiver error: %v", dst.String(), err)
					return
				case ttl, ok := <-ttlCh:
					if !ok {
						log.Printf("In ICMPSending goroutine for %s, no more TTL values will be generated", dst.String())
						return
					}

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
					counterStore.NumPktsSent.With(commonLabels).Add(1.0)

					numPktsSent++
					if pingRequest.TotalPkts != nil {
						if numPktsSent >= *pingRequest.TotalPkts {
							return
						}
					}
					<-time.After(time.Duration(pingRequest.IntvMilliseconds) * time.Millisecond)
				}
			}
		}()

		waitForICMPEVGenGoroutine.Wait()
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
