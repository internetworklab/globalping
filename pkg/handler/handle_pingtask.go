package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgutils "example.com/rbmq-demo/pkg/utils"
)

type PingTaskHandler struct {
	ConnRegistry    *pkgconnreg.ConnRegistry
	ClientTLSConfig *tls.Config
}

func getRemotePingerEndpoint(connRegistry *pkgconnreg.ConnRegistry, from string) *string {
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

	remotePingerEndpoint, ok := regData.Attributes[pkgnodereg.AttributeKeyHttpEndpoint]
	if ok && remotePingerEndpoint != "" {
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
			remotePingerEndpoint := getRemotePingerEndpoint(handler.ConnRegistry, from)
			if remotePingerEndpoint == nil {
				// the node might be currently offline, skip it for now
				log.Printf("Node %s appears on the conn registry's list but have no ping capability, skipping...", from)
				continue
			}

			derivedPingRequest := new(pkgpinger.SimplePingRequest)
			*derivedPingRequest = *form
			derivedPingRequest.Destination = target
			derivedPingRequest.From = []string{from}

			var remotePinger pkgpinger.Pinger = &pkgpinger.SimpleRemotePinger{
				Endpoint:        *remotePingerEndpoint,
				Request:         *derivedPingRequest,
				ClientTLSConfig: handler.ClientTLSConfig,
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
