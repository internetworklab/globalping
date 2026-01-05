package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	pkgtcping "example.com/rbmq-demo/pkg/tcping"
)

var (
	hostport = flag.String("hostport", "127.0.0.1:80", "host:port to ping")
	intvMs   = flag.Int("intvMs", 1000, "interval between pings in milliseconds")
	inetPref = flag.String("inetPref", "ip", "ip family preference: ip, ipv4, or ipv6")
)

func init() {
	flag.Parse()
}

func main() {

	ctx := context.Background()

	host, port, err := net.SplitHostPort(*hostport)
	if err != nil {
		log.Fatalf("failed to split host and port: %v", err)
	}

	resolver := net.DefaultResolver
	dstIPs, err := resolver.LookupIP(ctx, *inetPref, host)
	if err != nil {
		log.Fatalf("failed to lookup ip: %v", err)
	}

	if len(dstIPs) == 0 {
		log.Fatalf("no ip found for %s", host)
	}

	dstIP := dstIPs[0]
	dstPort, err := strconv.Atoi(port)
	if err != nil {
		log.Fatalf("failed to convert port to int: %v", err)
	}

	log.Printf("Pinging %s", net.JoinHostPort(dstIP.String(), strconv.Itoa(dstPort)))

	var sender pkgtcping.Sender
	if dstIP.To4() == nil {
		sender6, err := pkgtcping.NewTCPSYNSender6(ctx)
		if err != nil {
			log.Fatalf("failed to create ipv6 tcp syn sender: %v", err)
		}
		defer sender6.Close()
		sender = sender6
	} else {
		sender4, err := pkgtcping.NewTCPSYNSender(ctx)
		if err != nil {
			log.Fatalf("failed to create ipv4 tcp syn sender: %v", err)
		}
		defer sender4.Close()
		sender = sender4
	}

	rbCh := sender.GetPackets()

	requireSYN := true
	requireACK := true
	filteredCh := pkgtcping.FilterPackets(rbCh, &pkgtcping.FilterRequirements{
		SYN:     &requireSYN,
		ACK:     &requireACK,
		SrcPort: &dstPort,
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	trackerConfig := &pkgtcping.TrackerConfig{}
	tracker := pkgtcping.NewTracker(trackerConfig)
	tracker.Run(ctx)

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-tracker.EventC:
				if !ok {
					return
				}

				switch event.Type {
				case pkgtcping.TrackerEVTimeout:
					log.Printf("timeout, it was: %s", event.Entry.Value.String())
				case pkgtcping.TrackerEVReceived:
					log.Printf("got reply: %s, rtt: %s, ttl: %d, size: %d, it was: %s",
						event.Entry.Value.ReceivedPkt.String(),
						event.Entry.Value.RTT.String(),
						event.Entry.Value.ReceivedPkt.TTL,
						event.Entry.Value.ReceivedPkt.Size,
						event.Entry.Value.String(),
					)
				}
			}
		}
	}(ctx)

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case pktInfo, ok := <-filteredCh:
				if !ok {
					log.Printf("filteredCh is closed")
					return
				}

				tracker.MarkReceived(pktInfo)
			}
		}
	}(ctx)

	go func() {
		ticker := time.NewTicker(time.Duration(*intvMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				initSeqNum := rand.Intn(0x100000000)
				synRequest := &pkgtcping.TCPSYNRequest{
					DstIP:   dstIP,
					DstPort: dstPort,
					Timeout: 3 * time.Second,
					Seq:     uint32(initSeqNum),
					Ack:     0,
					Window:  0xffff,
				}
				_, err := sender.Send(synRequest, tracker)
				if err != nil {
					log.Fatalf("failed to send tcp syn: %v", err)
				}
				// log.Printf("sent syn: %s", receipt.String())
			}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	log.Printf("received signal: %s", sig.String())
}
