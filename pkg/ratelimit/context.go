package ratelimit

import (
	"context"
	"net/http"

	pkgutils "example.com/rbmq-demo/pkg/utils"
)

func WithRatelimiters(originalHandler http.Handler, pool RateLimitPool, enforcer RateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, pkgutils.CtxKeySharedRateLimitPool, pool)
		ctx = context.WithValue(ctx, pkgutils.CtxKeySharedRateLimitEnforcer, enforcer)
		r = r.WithContext(ctx)
		originalHandler.ServeHTTP(w, r)
	})
}
