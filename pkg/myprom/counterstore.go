package myprom

import (
	"context"
	"net/http"

	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type CounterStore struct {
	StartedTime       prometheus.Gauge
	ServedDurationMs  *prometheus.CounterVec
	NumRequestsServed *prometheus.CounterVec
	NumPktsSent       *prometheus.CounterVec
	NumPktsReceived   *prometheus.CounterVec
	NumBytesSent      *prometheus.CounterVec
	NumBytesReceived  *prometheus.CounterVec
}

const (
	PromLabelFrom   = "from"
	PromLabelTarget = "target"
	PromLabelClient = "client"
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

	cs.NumRequestsServed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_requests_served",
			Help: "The number of requests served by the globalping system",
		},
		commonLabels,
	)

	cs.NumPktsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_pkts_sent",
			Help: "The number of packets sent by the globalping system",
		},
		commonLabels,
	)

	cs.NumPktsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_pkts_received",
			Help: "The number of packets received by the globalping system",
		},
		commonLabels,
	)

	cs.NumBytesSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_bytes_sent",
			Help: "The number of bytes sent by the globalping system",
		},
		commonLabels,
	)

	cs.NumBytesReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "globalping_num_bytes_received",
			Help: "The number of bytes received by the globalping system",
		},
		commonLabels,
	)

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
