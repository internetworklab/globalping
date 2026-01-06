package main

import (
	"context"
	"flag"
	"log"
	"net"
	"strconv"

	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgtcping "example.com/rbmq-demo/pkg/tcping"
)

var (
	hostport     = flag.String("hostport", "127.0.0.1:80", "host:port to ping")
	intvMs       = flag.Int("intvMs", 1000, "interval between pings in milliseconds")
	pktTimeoutMs = flag.Int("pktTimeoutMs", 3000, "packet timeout in milliseconds")
	inetPref     = flag.String("inetPref", "ip", "ip family preference: ip, ip4, or ip6")
	count        = flag.Int("count", -1, "number of pings to send")
)

func init() {
	flag.Parse()
}

func main() {
	pingRequest := &pkgpinger.SimplePingRequest{
		Destination:            *hostport,
		IntvMilliseconds:       *intvMs,
		PktTimeoutMilliseconds: *pktTimeoutMs,
	}
	if *count > 0 {
		pingRequest.TotalPkts = count
	}
	if *inetPref != "ip" {
		itstrue := true
		if *inetPref == "ip4" {
			pingRequest.PreferV4 = &itstrue
		} else if *inetPref == "ip6" {
			pingRequest.PreferV6 = &itstrue
		}
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pinger := &pkgpinger.TCPSYNPinger{
		PingRequest: pingRequest,
	}
	for ev := range pinger.Ping(ctx) {
		if ev.Error != nil {
			log.Fatalf("error: %v", ev.Error)
		}
		if ev.Data != nil {
			if tcpTrackEv, ok := ev.Data.(pkgtcping.TrackerEvent); ok {
				switch tcpTrackEv.Type {
				case pkgtcping.TrackerEVTimeout:
					log.Printf("timeout: seq=%v", tcpTrackEv.Entry.Value.Seq)
				case pkgtcping.TrackerEVReceived:
					from := net.JoinHostPort(tcpTrackEv.Entry.Value.SrcIP.String(), strconv.Itoa(tcpTrackEv.Entry.Value.SrcPort))
					to := net.JoinHostPort(tcpTrackEv.Entry.Value.Request.DstIP.String(), strconv.Itoa(tcpTrackEv.Entry.Value.Request.DstPort))
					log.Printf("received: seq=%v, rtt=%v, ttl=%v, %s <- %s", tcpTrackEv.Entry.Value.Seq, tcpTrackEv.Entry.Value.RTT, tcpTrackEv.Entry.Value.ReceivedPkt.TTL, from, to)
				}
			}
		}
	}
}
