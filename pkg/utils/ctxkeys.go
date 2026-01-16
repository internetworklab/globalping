package utils

type CtxKey string

const (
	CtxKeyPrometheusCounterStore = CtxKey("prometheus_counter_store")
	CtxKeyPromCommonLabels       = CtxKey("prom_common_labels")
	CtxKeyStartedAt              = CtxKey("started_at")
)

type GlobalSharedContext struct {
	BuildVersion *BuildVersion
}
