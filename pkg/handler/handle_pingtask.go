package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgutils "example.com/rbmq-demo/pkg/utils"
	quicHttp3 "github.com/quic-go/quic-go/http3"
)

type OutOfRespondRangePolicy string

const (
	ORPolicyAllow = "allow"
	ORPolicyDeny  = "deny"
)

type PingTaskHandler struct {
	ConnRegistry            *pkgconnreg.ConnRegistry
	ClientTLSConfig         *tls.Config
	Resolver                *net.Resolver
	OutOfRespondRangePolicy OutOfRespondRangePolicy
	MinPktInterval          *time.Duration
	MaxPktTimeout           *time.Duration
	PktCountClamp           *int
}

const (
	defaultRemotePingerPath = "/simpleping"
)

func getConnWithCapability(ctx context.Context, connRegistry *pkgconnreg.ConnRegistry, from string, capability string) *pkgconnreg.ConnRegistryData {
	if connRegistry == nil {
		return nil
	}

	regData, err := connRegistry.SearchByAttributes(pkgconnreg.ConnectionAttributes{
		pkgnodereg.AttributeKeyPingCapability: "true",
		pkgnodereg.AttributeKeyNodeName:       from,
	})
	if err != nil {
		log.Printf("Failed to search by attributes: %v", err)
		return nil
	}

	if regData == nil {
		// didn't found
		return nil
	}

	return regData.Clone()
}

func tryStripPort(addrport string) string {
	host, _, err := net.SplitHostPort(addrport)
	if err == nil {
		return host
	}
	return addrport
}

func checkRemotePingerPolicy(ctx context.Context, regData *pkgconnreg.ConnRegistryData, target string, resolver *net.Resolver, outOfRangePolicy OutOfRespondRangePolicy) (*string, *http.Client) {
	// When OutOfRange policy is 'deny', the hub will carefully consider the RespondRange attribute announced by the agent,
	// and make sure the ping request won't be distributed to whom that are not desired.
	if outOfRangePolicy == ORPolicyDeny && regData.Attributes[pkgnodereg.AttributeKeyRespondRange] != "" {
		respondRange := strings.Split(regData.Attributes[pkgnodereg.AttributeKeyRespondRange], ",")
		rangeCIDRs := make([]net.IPNet, 0)
		for _, rangeStr := range respondRange {
			rangeStr = strings.TrimSpace(rangeStr)
			if rangeStr == "" {
				continue
			}
			if _, nw, err := net.ParseCIDR(rangeStr); err == nil && nw != nil {
				rangeCIDRs = append(rangeCIDRs, *nw)
			}
		}

		var dsts []net.IP
		var err error
		dsts, err = resolver.LookupIP(ctx, "ip", target)
		if err != nil {
			log.Printf("Failed to lookup IP for target %s: %v", target, err)
			dsts = make([]net.IP, 0)
		}
		if len(rangeCIDRs) > 0 && !pkgutils.CheckIntersect(dsts, rangeCIDRs) {
			log.Printf("Target %s is not in the respond range of node %+v which is %s", target, regData.NodeName, strings.Join(respondRange, ", "))
			log.Printf("Out of range target %s will not be assigned to a remote pinger because of the policy", target)
			return nil, nil
		}
	}

	if regData.QUICConn != nil {
		tr := &quicHttp3.Transport{}
		httpClient := &http.Client{
			Transport: tr.NewRawClientConn(regData.QUICConn),
		}
		log.Printf("Using QUIC connection to remote pinger %s", target)
		return nil, httpClient
	}

	remotePingerEndpoint, ok := regData.Attributes[pkgnodereg.AttributeKeyHttpEndpoint]
	if ok && remotePingerEndpoint != "" {
		urlObj, err := url.Parse(remotePingerEndpoint)
		if err != nil {
			log.Printf("Failed to parse remote pinger endpoint: %v", err)
			return nil, nil
		}
		if urlObj.Path == "" {
			urlObj.Path = defaultRemotePingerPath
		}
		remotePingerEndpoint = urlObj.String()
		return &remotePingerEndpoint, nil
	}

	return nil, nil
}

func RespondError(w http.ResponseWriter, err error, status int) {
	respbytes, err := json.Marshal(pkgutils.ErrorResponse{Error: err.Error()})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Error(w, string(respbytes), status)
}

type withMetadataPinger struct {
	origin   pkgpinger.Pinger
	metadata map[string]string
}

func (wmp *withMetadataPinger) Ping(ctx context.Context) <-chan pkgpinger.PingEvent {
	if wmp.metadata == nil {
		return wmp.origin.Ping(ctx)
	}
	wrappedCh := make(chan pkgpinger.PingEvent)
	go func() {
		defer close(wrappedCh)

		for ev := range wmp.origin.Ping(ctx) {
			wrappedEv := new(pkgpinger.PingEvent)
			*wrappedEv = ev
			if wrappedEv.Metadata == nil {
				wrappedEv.Metadata = make(map[string]string)
			}
			for k, v := range wmp.metadata {
				wrappedEv.Metadata[k] = v
			}
			wrappedCh <- *wrappedEv
		}
	}()
	return wrappedCh
}

func WithMetadata(pinger pkgpinger.Pinger, metadata map[string]string) pkgpinger.Pinger {
	return &withMetadataPinger{
		origin:   pinger,
		metadata: metadata,
	}
}

func getRealPktTimeout(req *http.Request, pingReq *pkgpinger.SimplePingRequest, maxPktTimeout *time.Duration) time.Duration {
	if maxPktTimeout == nil {
		return time.Duration(pingReq.PktTimeoutMilliseconds) * time.Millisecond
	}

	pktTimeoutMs := int64(pingReq.PktTimeoutMilliseconds)
	if pktTimeoutMs > maxPktTimeout.Milliseconds() {
		log.Printf("Request from %s has a too big pkt timeout %dms, clamping to %dms", pkgutils.GetRemoteAddr(req), pktTimeoutMs, maxPktTimeout.Milliseconds())
		pktTimeoutMs = maxPktTimeout.Milliseconds()
	}
	return time.Duration(pktTimeoutMs) * time.Millisecond
}

func getRealPktIntv(req *http.Request, pingReq *pkgpinger.SimplePingRequest, minPktInterval *time.Duration) time.Duration {
	if minPktInterval == nil {
		return time.Duration(pingReq.IntvMilliseconds) * time.Millisecond
	}

	intvMs := int64(pingReq.IntvMilliseconds)
	if intvMs < minPktInterval.Milliseconds() {
		log.Printf("Request from %s has a too small pkt interval %dms, clamping to %dms", pkgutils.GetRemoteAddr(req), intvMs, minPktInterval.Milliseconds())
		intvMs = minPktInterval.Milliseconds()
	}
	return time.Duration(intvMs) * time.Millisecond
}

func (handler *PingTaskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set headers for streaming response
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	form, err := pkgpinger.ParseSimplePingRequest(r)
	if err != nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: err.Error()})
		return
	}

	form.IntvMilliseconds = int(getRealPktIntv(r, form, handler.MinPktInterval).Milliseconds())
	form.PktTimeoutMilliseconds = int(getRealPktTimeout(r, form, handler.MaxPktTimeout).Milliseconds())
	if handler.PktCountClamp != nil {
		if form.TotalPkts == nil {
			form.TotalPkts = new(int)
			*form.TotalPkts = *handler.PktCountClamp
			log.Printf("Request from %s has no pkt count specified, clamping to %d", pkgutils.GetRemoteAddr(r), *handler.PktCountClamp)
		} else if *form.TotalPkts > *handler.PktCountClamp {
			log.Printf("Request from %s has a too big pkt count %d, clamping to %d", pkgutils.GetRemoteAddr(r), *form.TotalPkts, *handler.PktCountClamp)
			*form.TotalPkts = *handler.PktCountClamp
		}
	}

	pingers := make(map[string]map[string]pkgpinger.Pinger, 0)
	ctx := r.Context()

	if form.From == nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: "you must specify at least one from node"})
		return
	}
	if form.Targets == nil && form.L7PacketType == nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: "you must specify at least one target"})
		return
	}

	extraRequestHeader := make(map[string]string)
	extraRequestHeader["X-Forwarded-For"] = pkgutils.GetRemoteAddr(r)
	extraRequestHeader["X-Real-IP"] = pkgutils.GetRemoteAddr(r)

	for _, from := range form.From {
		remotePingable := getConnWithCapability(ctx, handler.ConnRegistry, from, pkgnodereg.AttributeKeyPingCapability)
		if remotePingable != nil {
			for _, target := range form.Targets {
				remotePingerEndpoint, quicClient := checkRemotePingerPolicy(ctx, remotePingable, target, handler.Resolver, handler.OutOfRespondRangePolicy)
				if remotePingerEndpoint == nil && quicClient == nil {
					continue
				}

				sp := &pkgpinger.SimpleRemotePinger{
					Request:            *form.DeriveAsPingRequest(from, target),
					ClientTLSConfig:    handler.ClientTLSConfig,
					ExtraRequestHeader: extraRequestHeader,
					QUICClient:         quicClient,
					NodeName:           from,
				}

				if remotePingerEndpoint != nil {
					log.Printf("Sending ping to remote pinger %s via http endpoint %s", from, *remotePingerEndpoint)
					sp.Endpoint = *remotePingerEndpoint
				}

				var remotePinger pkgpinger.Pinger = sp
				if _, ok := pingers[from]; !ok {
					pingers[from] = make(map[string]pkgpinger.Pinger, 0)
				}
				pingers[from][target] = remotePinger
			}
		}

		dnsProbeable := getConnWithCapability(ctx, handler.ConnRegistry, from, pkgnodereg.AttributeKeyDNSProbeCapability)
		if dnsProbeable != nil {
			for _, dnsTarget := range form.DNSTargets {
				corrId := dnsTarget.CorrelationID
				if corrId == "" {
					log.Printf("invalid dns target from %s: correlation id is empty", pkgutils.GetRemoteAddr(r))
					continue
				}
				if dnsTarget.AddrPort == "" {
					log.Printf("invalid dns target from %s: addrport is empty", pkgutils.GetRemoteAddr(r))
					continue
				}

				remotePingerEndpoint, quicClient := checkRemotePingerPolicy(ctx, remotePingable, tryStripPort(dnsTarget.AddrPort), handler.Resolver, handler.OutOfRespondRangePolicy)
				if remotePingerEndpoint == nil && quicClient == nil {
					continue
				}

				sp := &pkgpinger.SimpleRemotePinger{
					Request:            *form.DeriveAsDNSProbeRequest(from, dnsTarget),
					ClientTLSConfig:    handler.ClientTLSConfig,
					ExtraRequestHeader: extraRequestHeader,
					QUICClient:         quicClient,
					NodeName:           from,
				}
				var remotePinger pkgpinger.Pinger = sp
				if remotePingerEndpoint != nil {
					log.Printf("Sending ping to remote pinger %s via http endpoint %s", from, *remotePingerEndpoint)
					sp.Endpoint = *remotePingerEndpoint
				}

				if _, ok := pingers[from]; !ok {
					pingers[from] = make(map[string]pkgpinger.Pinger, 0)
				}
				pingers[from][corrId] = remotePinger
			}
		}
	}

	if len(pingers) == 0 {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: "no pingers to start"})
		return
	}

	pingersFlat := make([]pkgpinger.Pinger, 0)
	for from, submap := range pingers {
		for target, remotePinger := range submap {
			pingersFlat = append(pingersFlat, WithMetadata(remotePinger, map[string]string{
				pkgpinger.MetadataKeyFrom:   from,
				pkgpinger.MetadataKeyTarget: target,
			}))
		}
	}

	// Start multiple pings in parallel, and stream events as line-delimited JSON
	encoder := json.NewEncoder(w)
	for ev := range pkgpinger.StartMultiplePings(ctx, pingersFlat) {
		if err := encoder.Encode(ev); err != nil {
			log.Printf("Failed to encode event: %v", err)
			encoder.Encode(pkgutils.ErrorResponse{Error: fmt.Errorf("failed to encode event: %w", err).Error()})
			pkgutils.TryFlush(w)
			return
		}

		pkgutils.TryFlush(w)
	}
}
