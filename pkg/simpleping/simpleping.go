package simpleping

import (
	"context"
	"log"
	"time"

	"encoding/json"

	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	probing "github.com/prometheus-community/pro-bing"
)

type PktRepresentation struct {
	Rtt    uint64 `json:"rtt"`
	Addr   string `json:"ip_addr"`
	Nbytes int    `json:"nbytes"`
	Seq    int    `json:"seq"`
	TTL    int    `json:"ttl"`
	ID     int    `json:"id"`
	Dup    bool   `json:"dup"`
}

func (pktrepr *PktRepresentation) String() string {
	j, err := json.Marshal(pktrepr)
	if err != nil {
		log.Printf("Failed to marshal pkt: %v", err)
		return ""
	}
	return string(j)
}

func NewPktRepresentation(pkt *probing.Packet, dup bool) *PktRepresentation {

	r := PktRepresentation{
		Rtt:    uint64(pkt.Rtt.Milliseconds()),
		Addr:   pkt.Addr,
		Nbytes: pkt.Nbytes,
		Seq:    pkt.Seq,
		TTL:    pkt.TTL,
		ID:     pkt.ID,
		Dup:    dup,
	}

	return &r
}

type PingStatsRepresentation struct {
	// PacketsRecv is the number of packets received.
	PacketsRecv int `json:"packets_recv"`

	// PacketsSent is the number of packets sent.
	PacketsSent int `json:"packets_sent"`

	// PacketsRecvDuplicates is the number of duplicate responses there were to a sent packet.
	PacketsRecvDuplicates int `json:"packets_recv_duplicates"`

	// PacketLoss is the percentage of packets lost.
	PacketLoss float64 `json:"packet_loss"`

	// IPAddr is the address of the host being pinged.
	IPAddr string `json:"ip_addr"`

	// Rtts is all of the round-trip times sent via this pinger.
	Rtts []int `json:"rtts"`

	// TTLs is all of the TTLs received via this pinger.
	TTLs []int `json:"ttls"`

	// MinRtt is the minimum round-trip time sent via this pinger.
	MinRtt uint64 `json:"min_rtt"`

	// MaxRtt is the maximum round-trip time sent via this pinger.
	MaxRtt uint64 `json:"max_rtt"`

	// AvgRtt is the average round-trip time sent via this pinger.
	AvgRtt uint64 `json:"avg_rtt"`

	// StdDevRtt is the standard deviation of the round-trip times sent via
	// this pinger.
	StdDevRtt uint64 `json:"std_dev_rtt"`
}

func NewPingStatsRepresentation(stats *probing.Statistics) *PingStatsRepresentation {
	r := PingStatsRepresentation{
		PacketsRecv:           stats.PacketsRecv,
		PacketsSent:           stats.PacketsSent,
		PacketsRecvDuplicates: stats.PacketsRecvDuplicates,
		PacketLoss:            stats.PacketLoss,
		IPAddr:                stats.IPAddr.String(),
	}
	rtts := make([]int, 0)
	for _, rtt := range stats.Rtts {
		rtts = append(rtts, int(rtt.Milliseconds()))
	}
	r.Rtts = rtts
	ttls := make([]int, 0)
	for _, ttl := range stats.TTLs {
		ttls = append(ttls, int(ttl))
	}
	r.TTLs = ttls
	r.MinRtt = uint64(stats.MinRtt.Milliseconds())
	r.MaxRtt = uint64(stats.MaxRtt.Milliseconds())
	r.AvgRtt = uint64(stats.AvgRtt.Milliseconds())
	r.StdDevRtt = uint64(stats.StdDevRtt.Milliseconds())
	return &r
}

func (statsrepr *PingStatsRepresentation) String() string {
	j, err := json.Marshal(statsrepr)
	if err != nil {
		log.Printf("Failed to marshal stats: %v", err)
		return ""
	}
	return string(j)
}

type PingConfiguration struct {
	Destination string        `json:"destination"`
	Count       int           `json:"count"`
	Timeout     time.Duration `json:"timeout"`
	Interval    time.Duration `json:"interval"`
}

type SimplePinger struct {
	cfg PingConfiguration
}

func NewSimplePinger(cfg *PingConfiguration) *SimplePinger {
	return &SimplePinger{
		cfg: *cfg,
	}
}

func (p *SimplePinger) Ping(ctx context.Context) <-chan pkgpinger.PingEvent {
	return startPinging(&p.cfg)
}

// startPinging starts pinging the given destination and returns a channel
// that will receive all ping events
func startPinging(cfg *PingConfiguration) <-chan pkgpinger.PingEvent {
	destination := cfg.Destination

	eventCh := make(chan pkgpinger.PingEvent)

	go func() {
		defer close(eventCh)

		pinger, err := probing.NewPinger(destination)
		if err != nil {
			log.Printf("Failed to create pinger: %v", err)
			return
		}

		pinger.Count = cfg.Count
		pinger.Timeout = cfg.Timeout
		pinger.Interval = cfg.Interval

		pinger.OnRecv = func(pkt *probing.Packet) {
			ev := pkgpinger.PingEvent{
				Type: pkgpinger.PingEventTypePktRecv,
				Data: NewPktRepresentation(pkt, false),
			}
			eventCh <- ev
		}

		pinger.OnDuplicateRecv = func(pkt *probing.Packet) {
			ev := pkgpinger.PingEvent{
				Type: pkgpinger.PingEventTypePktDupRecv,
				Data: NewPktRepresentation(pkt, true),
			}
			eventCh <- ev
		}

		pinger.OnFinish = func(stats *probing.Statistics) {
			ev := pkgpinger.PingEvent{
				Type: pkgpinger.PingEventTypePingStats,
				Data: NewPingStatsRepresentation(stats),
			}
			eventCh <- ev
		}

		pinger.SetPrivileged(true)
		err = pinger.Run()
		if err != nil {
			log.Printf("Failed to run pinger: %v", err)
		}
	}()

	return eventCh
}
