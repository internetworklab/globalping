package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgratelimit "example.com/rbmq-demo/pkg/ratelimit"
	pkgraw "example.com/rbmq-demo/pkg/raw"
)

func rateLimitIO(ctx context.Context, inC chan<- pkgraw.ICMPSendRequest, rl pkgratelimit.RateLimiter) chan<- pkgraw.ICMPSendRequest {
	rlIn, rlOut, _ := rl.GetIO(ctx)

	go func() {
		for item := range rlOut {
			inC <- item.(pkgraw.ICMPSendRequest)
		}
	}()

	wrappedInC := make(chan pkgraw.ICMPSendRequest)
	go func() {
		defer close(rlIn)
		for item := range wrappedInC {
			rlIn <- item
		}
	}()

	return wrappedInC
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := 32306
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   nil,
		Port: port,
	})
	if err != nil {
		log.Fatalf("failed to listen on tcp: %v", err)
	}
	defer listener.Close()
	log.Printf("listening on tcp port %d", port)

	refreshIntv, _ := time.ParseDuration("2s")
	numTokensPerPeriod := 3

	ratelimitPool := &pkgratelimit.MemoryBasedRateLimitPool{
		RefreshIntv:     refreshIntv,
		NumTokensPerKey: numTokensPerPeriod,
	}
	ratelimitPool.Run(ctx)

	// since all requests yield the same key, so this is considered as a globally shared rate limiter
	globalSharedRL := &pkgratelimit.MemoryBasedRateLimiter{
		Pool:   ratelimitPool,
		GetKey: pkgratelimit.GlobalKeyFunc,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		destination := r.URL.Query().Get("destination")
		if destination == "" {
			http.Error(w, "destination is required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		resolver := net.DefaultResolver

		ipVersion := "ip"

		if prefer := r.URL.Query().Get("prefer"); prefer != "" {
			if prefer == "ipv4" {
				ipVersion = "ip4"
			} else if prefer == "ipv6" {
				ipVersion = "ip6"
			} else {
				http.Error(w, "invalid prefer value, must be ipv4 or ipv6", http.StatusBadRequest)
				return
			}
		}

		log.Printf("using ip version: %s", ipVersion)

		dstIP, err := resolver.LookupIP(ctx, ipVersion, destination)
		if err != nil {
			log.Fatalf("failed to lookup IP for domain %s: %v", destination, err)
		}
		if len(dstIP) == 0 {
			log.Fatalf("no IP found for domain %s", destination)
		}

		log.Printf("lookup IP for domain %s: %v", destination, dstIP)

		var icmpTr pkgraw.GeneralICMPTransceiver
		if dstIP[0].To4() != nil {
			icmp4tr, err := pkgraw.NewICMP4Transceiver(pkgraw.ICMP4TransceiverConfig{
				UseUDP:      false,
				UDPBasePort: nil,
				OnSent:      nil,
				OnReceived:  nil,
			})
			if err != nil {
				log.Fatalf("failed to create icmp4 transceiver: %v", err)
			}
			icmpTr = icmp4tr
		} else {
			icmp6tr, err := pkgraw.NewICMP6Transceiver(pkgraw.ICMP6TransceiverConfig{
				UseUDP:      false,
				UDPBasePort: nil,
				OnSent:      nil,
				OnReceived:  nil,
			})
			if err != nil {
				log.Fatalf("failed to create icmp6 transceiver: %v", err)
			}
			icmpTr = icmp6tr
		}

		inCRaw, outC, errC := icmpTr.GetIO(ctx)
		inC := rateLimitIO(ctx, inCRaw, globalSharedRL)
		defer close(inC)

		icmpRequest := pkgraw.ICMPSendRequest{
			Dst:        net.IPAddr{IP: dstIP[0]},
			Seq:        1,
			TTL:        64,
			Data:       nil,
			PMTU:       nil,
			NexthopMTU: 1500,
		}

		intv, _ := time.ParseDuration("1s")
		ticker := time.NewTicker(intv)
		defer ticker.Stop()
		seq := 1
		for {
			select {
			case <-ctx.Done():
				log.Printf("context done, no more pings to send")
				return
			case err, ok := <-errC:
				if ok && err != nil {
					log.Printf("error received: %v", err)
				}
				return
			case reply, ok := <-outC:
				if ok {
					if err := json.NewEncoder(w).Encode(reply); err != nil {
						log.Printf("failed to encode reply: %v", err)
						return
					}
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
				}
			case <-ticker.C:
				icmpRequest.Seq = seq
				seq++
				inC <- icmpRequest
				log.Printf("sent icmp request to %s, remote: %s", dstIP[0].String(), r.RemoteAddr)
			}
		}
	})

	server := &http.Server{
		Handler: handler,
	}

	go func() {
		log.Printf("starting server on port %d", port)
		if err := server.Serve(listener); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Fatalf("failed to serve: %v", err)
				return
			}
		}
	}()

	sigsCh := make(chan os.Signal, 1)
	signal.Notify(sigsCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigsCh
	log.Printf("signal received: %v, exiting...", sig)
}
