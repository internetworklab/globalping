package myprom

import (
	"context"
	"log"
	"net/http"

	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type CounterStore struct {
	StartedTime            prometheus.Gauge
	ServedDurationMs       *prometheus.CounterVec
	NumRequestsServed      *prometheus.CounterVec
	NumPktsSent            *prometheus.CounterVec
	NumPktsReceived        *prometheus.CounterVec
	NumBytesSent           *prometheus.CounterVec
	NumBytesReceived       *prometheus.CounterVec
	IPInfoRequests         *prometheus.CounterVec
	IPInfoServedDurationMs *prometheus.CounterVec
}

const (
	PromLabelFrom     = "from"
	PromLabelTarget   = "target"
	PromLabelClient   = "client"
	PromLabelCacheHit = "cachehit"
	PromLabelHasError = "haserror"
)

func NewCounterStore() *CounterStore {
	cs := new(CounterStore)
	cs.StartedTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "globalping_started_at",
		Help: "The time when the globalping system is started",
	})

	var commonLabels []string = []string{
		PromLabelFrom, PromLabelTarget, PromLabelClient,
	}

	cs.ServedDurationMs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_served_duration_ms",
			Help: "The duration of the requests served by the globalping system",
		},
		commonLabels,
	)
	if err := prometheus.Register(cs.ServedDurationMs); err != nil {
		log.Printf("ServedDurationMs might have been already registered: %v", err)
	}

	cs.NumRequestsServed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_requests_served",
			Help: "The number of requests served by the globalping system",
		},
		commonLabels,
	)
	if err := prometheus.Register(cs.NumRequestsServed); err != nil {
		log.Printf("NumRequestsServed might have been already registered: %v", err)
	}

	cs.NumPktsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_pkts_sent",
			Help: "The number of packets sent by the globalping system",
		},
		commonLabels,
	)
	if err := prometheus.Register(cs.NumPktsSent); err != nil {
		log.Printf("NumPktsSent might have been already registered: %v", err)
	}

	cs.NumPktsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_pkts_received",
			Help: "The number of packets received by the globalping system",
		},
		commonLabels,
	)
	if err := prometheus.Register(cs.NumPktsReceived); err != nil {
		log.Printf("NumPktsReceived might have been already registered: %v", err)
	}

	cs.NumBytesSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_bytes_sent",
			Help: "The number of bytes sent by the globalping system",
		},
		commonLabels,
	)
	if err := prometheus.Register(cs.NumBytesSent); err != nil {
		log.Printf("NumBytesSent might have been already registered: %v", err)
	}

	cs.NumBytesReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_bytes_received",
			Help: "The number of bytes received by the globalping system",
		},
		commonLabels,
	)
	if err := prometheus.Register(cs.NumBytesReceived); err != nil {
		log.Printf("NumBytesReceived might have been already registered: %v", err)
	}

	cs.IPInfoServedDurationMs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_ipinfo_served_duration_ms",
			Help: "The duration of the IPInfo requests served by the globalping system",
		},
		commonLabels,
	)
	if err := prometheus.Register(cs.IPInfoServedDurationMs); err != nil {
		log.Printf("IPInfoServedDurationMs might have been already registered: %v", err)
	}

	cs.IPInfoRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_ipinfo_requests",
			Help: "The number of IPInfo requests made by the globalping system",
		},
		append(commonLabels, PromLabelCacheHit, PromLabelHasError),
	)
	if err := prometheus.Register(cs.IPInfoRequests); err != nil {
		log.Printf("IPInfoRequests might have been already registered: %v", err)
	}

	return cs
}

func WithCounterStoreHandler(originalHandler http.Handler, counterStore *CounterStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, pkgutils.CtxKeyPrometheusCounterStore, counterStore)
		r = r.WithContext(ctx)
		originalHandler.ServeHTTP(w, r)
	})
}
