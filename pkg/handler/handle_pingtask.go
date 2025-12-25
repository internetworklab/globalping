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
}

const (
	defaultRemotePingerPath = "/simpleping"
)

func getRemotePingerEndpoint(ctx context.Context, connRegistry *pkgconnreg.ConnRegistry, from string, target string, resolver *net.Resolver, outOfRangePolicy OutOfRespondRangePolicy) *string {
	if connRegistry == nil {
		k := from
		return &k
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
		if !pkgutils.CheckIntersect(dsts, rangeCIDRs) {
			log.Printf("Target %s is not in the respond range of node %s, which is %s", target, from, strings.Join(respondRange, ", "))
			log.Printf("Out of range target %s will not be assigned to a remote pinger because of the policy", target)
			return nil
		}
	}

	remotePingerEndpoint, ok := regData.Attributes[pkgnodereg.AttributeKeyHttpEndpoint]
	if ok && remotePingerEndpoint != "" {
		urlObj, err := url.Parse(remotePingerEndpoint)
		if err != nil {
			log.Printf("Failed to parse remote pinger endpoint: %v", err)
			return nil
		}
		if urlObj.Path == "" {
			urlObj.Path = defaultRemotePingerPath
		}
		remotePingerEndpoint = urlObj.String()
		return &remotePingerEndpoint
	}

	return nil
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

	pingers := make([]pkgpinger.Pinger, 0)
	ctx := r.Context()

	if form.From == nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: "you must specify at least one from node"})
		return
	}
	if form.Targets == nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: "you must specify at least one target"})
		return
	}

	for _, from := range form.From {
		for _, target := range form.Targets {
			remotePingerEndpoint := getRemotePingerEndpoint(ctx, handler.ConnRegistry, from, target, handler.Resolver, handler.OutOfRespondRangePolicy)
			if remotePingerEndpoint == nil {
				// the node might be currently offline, skip it for now
				log.Printf("Node %s appears on the conn registry's list but have no ping capability, skipping...", from)
				continue
			}

			derivedPingRequest := new(pkgpinger.SimplePingRequest)
			*derivedPingRequest = *form
			derivedPingRequest.Destination = target
			derivedPingRequest.From = []string{from}

			extraRequestHeader := make(map[string]string)
			extraRequestHeader["X-Forwarded-For"] = pkgutils.GetRemoteAddr(r)
			extraRequestHeader["X-Real-IP"] = pkgutils.GetRemoteAddr(r)

			var remotePinger pkgpinger.Pinger = &pkgpinger.SimpleRemotePinger{
				Endpoint:           *remotePingerEndpoint,
				Request:            *derivedPingRequest,
				ClientTLSConfig:    handler.ClientTLSConfig,
				ExtraRequestHeader: extraRequestHeader,
			}
			remotePinger = WithMetadata(remotePinger, map[string]string{
				pkgpinger.MetadataKeyFrom:   from,
				pkgpinger.MetadataKeyTarget: target,
			})

			log.Printf("Sending ping to remote pinger %s via http endpoint %s", target, *remotePingerEndpoint)

			pingers = append(pingers, remotePinger)
		}
	}

	if len(pingers) == 0 {
		RespondError(w, fmt.Errorf("no pingers to start"), http.StatusInternalServerError)
		return
	}

	// Start multiple pings in parallel, and stream events as line-delimited JSON
	encoder := json.NewEncoder(w)
	for ev := range pkgpinger.StartMultiplePings(ctx, pingers) {
		if err := encoder.Encode(ev); err != nil {
			log.Printf("Failed to encode event: %v", err)
			encoder.Encode(pkgutils.ErrorResponse{Error: fmt.Errorf("failed to encode event: %w", err).Error()})
			pkgutils.TryFlush(w)
			return
		}

		pkgutils.TryFlush(w)
	}
}
