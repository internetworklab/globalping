package simpleping

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
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

	// Target is the target hostname in un-resolved form
	Target string `json:"target"`
}

func (pktrepr *PktRepresentation) String() string {
	j, err := json.Marshal(pktrepr)
	if err != nil {
		log.Printf("Failed to marshal pkt: %v", err)
		return ""
	}
	return string(j)
}

func NewPktRepresentation(pkt *probing.Packet, dup bool, dest string) *PktRepresentation {

	r := PktRepresentation{
		Rtt:    uint64(pkt.Rtt.Milliseconds()),
		Addr:   pkt.Addr,
		Nbytes: pkt.Nbytes,
		Seq:    pkt.Seq,
		TTL:    pkt.TTL,
		ID:     pkt.ID,
		Dup:    dup,
		Target: dest,
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

	// Target is the target hostname in un-resolved form
	Target string `json:"target"`

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

func NewPingStatsRepresentation(stats *probing.Statistics, dest string) *PingStatsRepresentation {
	r := PingStatsRepresentation{
		PacketsRecv:           stats.PacketsRecv,
		PacketsSent:           stats.PacketsSent,
		PacketsRecvDuplicates: stats.PacketsRecvDuplicates,
		PacketLoss:            stats.PacketLoss,
		IPAddr:                stats.IPAddr.String(),
		Target:                dest,
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
				Data: NewPktRepresentation(pkt, false, destination),
			}
			eventCh <- ev
		}

		pinger.OnDuplicateRecv = func(pkt *probing.Packet) {
			ev := pkgpinger.PingEvent{
				Type: pkgpinger.PingEventTypePktDupRecv,
				Data: NewPktRepresentation(pkt, true, destination),
			}
			eventCh <- ev
		}

		pinger.OnFinish = func(stats *probing.Statistics) {
			ev := pkgpinger.PingEvent{
				Type: pkgpinger.PingEventTypePingStats,
				Data: NewPingStatsRepresentation(stats, destination),
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

type SimpleRemotePinger struct {
	// A string use to identify the remote pinger itself (i.e. to identify which remote pinger that the packet is actually come from)
	from       string
	fullURL    *url.URL
	httpClient *http.Client
}

func encodeURLQueryForSimpleRemotePinger(cfg *PingConfiguration) url.Values {
	query := url.Values{}
	query.Add("destination", cfg.Destination)
	query.Add("count", strconv.Itoa(cfg.Count))
	query.Add("timeout", strconv.Itoa(int(cfg.Timeout.Seconds())))
	query.Add("interval", strconv.Itoa(int(cfg.Interval.Seconds())))
	return query
}

func extractAbsPathFromUNIXURL(u *url.URL) (string, error) {
	if u.Scheme != "unix" {
		return "", fmt.Errorf("scheme of the provided URL is not unix: %q", u.Scheme)
	}
	if u.Host != "" {
		return "", fmt.Errorf("provided URL is invalid, it has a non-empty host field which is un-expected, valid unix URL: unix://<abs_path_to_sock_file e.g. unix:///path/to/somewhere/myprog.sock")
	}
	// Only allow a single form of unix socket to guarantee consistency
	return u.Path, nil
}

type RemotePingerSpec struct {
	// BaseURL of the API endpoint of the remote pinger, valid examples:
	// - unix:///var/run/simple-pinger.sock
	// - http://localhost:8080/ping
	// - https://[::1]:8080/ping
	// - https://example.com/api/v1
	BaseURL string

	// HTTP request path of the API endpoint of the remote pinger, valid examples:
	// - /ping
	// - /some/path/to/ping
	// Note: no scheme, host or port are allowed here, only the path should be specified.
	HTTPPath string
}

// `remoteEndpoint“ specifies how to reach the api endpoint of the remote pinger
// `cfg` specifies ping what
// `from` specifies the name of the remote pinger itself so that the caller can identify which remote pinger that the packet/event is actually come from
func NewSimpleRemotePinger(spec RemotePingerSpec, cfg *PingConfiguration, from string) (*SimpleRemotePinger, error) {

	baseURL, err := url.Parse(spec.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote endpoint: %w", err)
	}
	if baseURL.Scheme == "http" || baseURL.Scheme == "https" {
		baseURL.RawQuery = encodeURLQueryForSimpleRemotePinger(cfg).Encode()
		baseURL.Path, err = url.JoinPath(baseURL.Path, spec.HTTPPath)
		if err != nil {
			return nil, fmt.Errorf("failed to join path: %w, base URL: %q, path: %q", err, spec.BaseURL, spec.HTTPPath)
		}
		log.Printf("Will use API endpoint: %s", baseURL.String())
		return &SimpleRemotePinger{
			from:       from,
			httpClient: http.DefaultClient,
			fullURL:    baseURL,
		}, nil
	}
	if baseURL.Scheme == "unix" {
		customTransport := &http.Transport{}
		dialer := &net.Dialer{}
		unixSockPath, err := extractAbsPathFromUNIXURL(baseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to extract absolute path from UNIX URL: %w, base URL: %q", err, spec.BaseURL)
		}
		log.Printf("Will use UNIX socket: %s", unixSockPath)
		customTransport.DialContext = func(ctx context.Context, nw, add string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", unixSockPath)
		}
		httpClient := &http.Client{
			Transport: customTransport,
		}
		return &SimpleRemotePinger{
			httpClient: httpClient,
			fullURL: &url.URL{
				Scheme: "http",
				Host:   "localhost",
				Path:   spec.HTTPPath,
			},
			from: from,
		}, nil
	}
	return nil, fmt.Errorf("unsupported scheme for remote endpoint: %q, base URL: %q", baseURL.Scheme, spec.BaseURL)
}

func (srPinger *SimpleRemotePinger) Ping(ctx context.Context) <-chan pkgpinger.PingEvent {

	evChan := make(chan pkgpinger.PingEvent)

	go func() {
		defer close(evChan)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srPinger.fullURL.String(), nil)
		if err != nil {
			evChan <- pkgpinger.PingEvent{
				Type:  pkgpinger.PingEventTypeError,
				Error: err,
			}
			return
		}
		resp, err := srPinger.httpClient.Do(req)
		if err != nil {
			evChan <- pkgpinger.PingEvent{
				Type:  pkgpinger.PingEventTypeError,
				Error: err,
			}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			evChan <- pkgpinger.PingEvent{
				Type:  pkgpinger.PingEventTypeError,
				Error: fmt.Errorf("unexpected status code: %d", resp.StatusCode),
			}
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			var ev pkgpinger.PingEvent
			if err := json.Unmarshal(line, &ev); err != nil {
				evChan <- pkgpinger.PingEvent{
					Type:  pkgpinger.PingEventTypeError,
					Error: fmt.Errorf("failed to decode event line: %w", err),
				}
				continue
			}
			if ev.Metadata == nil {
				ev.Metadata = make(map[string]string)
			}
			ev.Metadata[pkgpinger.MetadataKeyFrom] = srPinger.from
			evChan <- ev
		}
		if err := scanner.Err(); err != nil {
			evChan <- pkgpinger.PingEvent{
				Type:  pkgpinger.PingEventTypeError,
				Error: err,
			}
			return
		}
	}()

	return evChan
}
