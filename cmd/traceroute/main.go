package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	pkgraw "example.com/rbmq-demo/pkg/raw"
	pkgthrottle "example.com/rbmq-demo/pkg/throttle"
	pkgutils "example.com/rbmq-demo/pkg/utils"
)

var socketPath = flag.String("socket-path", "/var/run/traceroute.sock", "path to the socket file")
var sharedQuota = flag.Int("shared-quota", 3, "shared quota for the traceroute (packets per second)")

func init() {
	flag.Parse()
}

type SimplePingRequest struct {
	ICMPId                 int
	Destination            string
	IntvMilliseconds       int
	PktTimeoutMilliseconds int
	PreferV4               *bool
	PreferV6               *bool
	TotalPkts              *int
	Resolver               *string
	TTL                    *int
}

func ParseSimplePingRequest(r *http.Request) (*SimplePingRequest, error) {
	result := new(SimplePingRequest)
	if count := r.URL.Query().Get("count"); count != "" {
		countInt, err := strconv.Atoi(count)
		if err != nil {
			return nil, fmt.Errorf("failed to parse count: %v", err)
		}
		result.TotalPkts = &countInt
	}

	if intervalMilliSecs := r.URL.Query().Get("intervalMs"); intervalMilliSecs != "" {
		intervalInt, err := strconv.Atoi(intervalMilliSecs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse interval: %v", err)
		}
		result.IntvMilliseconds = intervalInt
	} else {
		result.IntvMilliseconds = 1000
	}

	if pktTimeoutMilliSecs := r.URL.Query().Get("pktTimeoutMs"); pktTimeoutMilliSecs != "" {
		pktTimeoutInt, err := strconv.Atoi(pktTimeoutMilliSecs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse pktTimeout: %v", err)
		}
		result.PktTimeoutMilliseconds = pktTimeoutInt
	} else {
		result.PktTimeoutMilliseconds = 3000
	}

	if ttl := r.URL.Query().Get("ttl"); ttl != "" {
		ttlInt, err := strconv.Atoi(ttl)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ttl: %v", err)
		}
		result.TTL = &ttlInt
	}

	if preferV4 := r.URL.Query().Get("preferV4"); preferV4 != "" {
		preferV4Bool, err := strconv.ParseBool(preferV4)
		if err != nil {
			return nil, fmt.Errorf("failed to parse preferV4: %v", err)
		}
		result.PreferV4 = &preferV4Bool
	}

	if preferV6 := r.URL.Query().Get("preferV6"); preferV6 != "" {
		preferV6Bool, err := strconv.ParseBool(preferV6)
		if err != nil {
			return nil, fmt.Errorf("failed to parse preferV6: %v", err)
		}
		result.PreferV6 = &preferV6Bool
	}

	if resolver := r.URL.Query().Get("resolver"); resolver != "" {
		result.Resolver = &resolver
	}

	if icmpId := r.URL.Query().Get("id"); icmpId != "" {
		idInt, err := strconv.Atoi(icmpId)
		if err != nil {
			return nil, fmt.Errorf("failed to parse id: %v", err)
		}
		result.ICMPId = idInt
	} else {
		result.ICMPId = rand.Intn(0x10000)
	}

	destination := r.URL.Query().Get("destination")
	if destination == "" {
		return nil, fmt.Errorf("destination is required")
	}
	result.Destination = destination

	return result, nil
}

type PingHandler struct {
	hub *pkgthrottle.SharedThrottleHub
}

func NewPingHandler(hub *pkgthrottle.SharedThrottleHub) *PingHandler {
	ph := new(PingHandler)
	ph.hub = hub

	return ph
}

func (ph *PingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pingRequest, err := ParseSimplePingRequest(r)
	if err != nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: err.Error()})
		return
	}
	pingReqJSB, _ := json.Marshal(pingRequest)
	log.Printf("Started ping request for %s: %s", pkgutils.GetRemoteAddr(r), string(pingReqJSB))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	pktTimeout := 3 * time.Second
	pktInterval := 1 * time.Second
	buffRedundancyFactor := 2
	trackerConfig := &pkgraw.ICMPTrackerConfig{
		PacketTimeout:                 pktTimeout,
		TimeoutChannelEventBufferSize: buffRedundancyFactor * int(pktTimeout.Seconds()/math.Max(1, pktInterval.Seconds())),
	}
	tracker, err := pkgraw.NewICMPTracker(trackerConfig)
	if err != nil {
		log.Fatalf("failed to create ICMP tracker: %v", err)
	}
	tracker.Run(ctx)

	var resolver *net.Resolver = net.DefaultResolver
	if pingRequest.Resolver != nil {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: 10 * time.Second,
				}
				return d.DialContext(ctx, network, *pingRequest.Resolver)
			},
		}
	}

	dstPtr, err := selectDstIP(ctx, resolver, pingRequest.Destination, pingRequest.PreferV4, pingRequest.PreferV6)
	if err != nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: err.Error()})
		return
	}

	if dstPtr == nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: "no destination IP found"})
		return
	}
	dst := *dstPtr

	var transceiver pkgraw.GeneralICMPTransceiver
	if dst.IP.To4() != nil {
		icmp4tr, err := pkgraw.NewICMP4Transceiver(pkgraw.ICMP4TransceiverConfig{
			ID: pingRequest.ICMPId,
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
			ID: pingRequest.ICMPId,
		})
		if err != nil {
			log.Fatalf("failed to create ICMP6 transceiver: %v", err)
		}
		if err := icmp6tr.Run(ctx); err != nil {
			log.Fatalf("failed to run ICMP6 transceiver: %v", err)
		}
		transceiver = icmp6tr
	}

	throttleProxySrc := make(chan interface{}, 0)
	proxyCh, err := ph.hub.CreateProxy(ctx, throttleProxySrc)
	if err != nil {
		log.Fatalf("failed to create proxy: %v", err)
	}

	go func() {
		defer log.Printf("Exitting response generating goroutine for %s", pkgutils.GetRemoteAddr(r))

		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-tracker.RecvEvC:
				json.NewEncoder(w).Encode(ev)
				flusher, ok := w.(http.Flusher)
				if ok {
					flusher.Flush()
				}
			}
		}
	}()

	go func() {
		defer log.Printf("Exitting ICMP receiver goroutine for %s", pkgutils.GetRemoteAddr(r))

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
	ttl := 64
	if pingRequest.TTL != nil {
		ttl = *pingRequest.TTL
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case reqraw := <-proxyCh:
				log.Printf("received request from proxy: %v", reqraw)
				req, ok := reqraw.(pkgraw.ICMPSendRequest)
				if !ok {
					log.Fatal("wrong format")
				}
				senderCh <- req
			}
		}
	}()

	for {
		select {
		case <-r.Context().Done():
			log.Printf("Exitting sender goroutine for %s", pkgutils.GetRemoteAddr(r))
			return
		default:
			numPktsSent++
			req := pkgraw.ICMPSendRequest{
				Seq: numPktsSent,
				TTL: ttl,
				Dst: dst,
			}
			throttleProxySrc <- req
			tracker.MarkSent(req.Seq)

			if pingRequest.TotalPkts != nil && numPktsSent >= *pingRequest.TotalPkts {
				break
			}
			<-time.After(time.Duration(pingRequest.IntvMilliseconds) * time.Millisecond)
		}
	}
}

func selectDstIP(ctx context.Context, resolver *net.Resolver, host string, preferV4 *bool, preferV6 *bool) (*net.IPAddr, error) {
	familyPrefer := "ip"
	if preferV6 != nil && *preferV6 {
		familyPrefer = "ip6"
	} else if preferV4 != nil && *preferV4 {
		familyPrefer = "ip4"
	}
	ips, err := resolver.LookupIP(ctx, familyPrefer, host)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup IP: %v", err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP found for host: %s", host)
	}
	dst := net.IPAddr{IP: ips[0]}
	return &dst, nil
}

func main() {
	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if *sharedQuota < 1 {
		log.Fatalf("shared quota must be greater than 0")
	}

	throttleConfig := pkgthrottle.TokenBasedThrottleConfig{
		RefreshInterval:       1 * time.Second,
		TokenQuotaPerInterval: *sharedQuota,
	}
	tsSched, err := pkgthrottle.NewTimeSlicedEVLoopSched(&pkgthrottle.TimeSlicedEVLoopSchedConfig{})
	if err != nil {
		log.Fatalf("failed to create time sliced event loop scheduler: %v", err)
	}
	tsSchedRunerr := tsSched.Run(ctx)

	throttle := pkgthrottle.NewTokenBasedThrottle(throttleConfig)
	throttle.Run()

	smoother := pkgthrottle.NewBurstSmoother(time.Duration(1000.0/float64(*sharedQuota)) * time.Millisecond)
	smoother.Run()

	hub := pkgthrottle.NewICMPTransceiveHub(&pkgthrottle.SharedThrottleHubConfig{
		TSSched:  tsSched,
		Throttle: throttle,
		Smoother: smoother,
	})
	hub.Run(ctx)

	handler := NewPingHandler(hub)

	listener, err := net.Listen("unix", *socketPath)
	if err != nil {
		log.Fatalf("failed to listen on socket %s: %v", *socketPath, err)
	}
	defer listener.Close()
	log.Printf("Listening on socket %s", *socketPath)

	go func() {
		muxer := http.NewServeMux()
		muxer.Handle("/simpleping", handler)
		server := http.Server{
			Handler: muxer,
		}
		if err := server.Serve(listener); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Fatalf("failed to serve: %v", err)
			}
			log.Println("Server exitted")
		}
		go func() {
			<-ctx.Done()
			log.Println("Shutting down server")
			server.Shutdown(ctx)
		}()
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	err = <-tsSchedRunerr
	if err != nil {
		log.Fatalf("failed to run time sliced event loop scheduler: %v", err)
	}
	log.Printf("Received signal: %v, exiting...", sig.String())
	cancel()
}
