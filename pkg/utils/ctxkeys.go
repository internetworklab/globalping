package utils

type CtxKey string

const (
	CtxKeyPrometheusCounterStore  = CtxKey("prometheus_counter_store")
	CtxKeyPromCommonLabels        = CtxKey("prom_common_labels")
	CtxKeyStartedAt               = CtxKey("started_at")
	CtxKeySharedRateLimitEnforcer = CtxKey("shared_rate_limit_enforcer")
	CtxKeySharedRateLimitPool     = CtxKey("shared_rate_limit_pool")
	CtxKeyJWTSecret               = CtxKey("jwt_secret")
	CtxKeyJWTToken                = CtxKey("jwt_token")
)

type GlobalSharedContext struct {
	BuildVersion *BuildVersion
}
