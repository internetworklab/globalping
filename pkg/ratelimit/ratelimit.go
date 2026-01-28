package ratelimit

import (
	"context"
)

type Keyable interface {
	GetRatelimitKey() string
}

type RateLimiter interface {

	// returns: (output channel)
	// output channel will be closed if:
	// 1. context is done
	// 2. or, input channel is closed
	GetIO(ctx context.Context, input chan Keyable) (chan interface{}, chan error)
}

type RateLimitPool interface {
	// returns false when quota is exhausted, true otherwise
	// the second return value is error, if any, such as, when timeout occurs
	Consume(ctx context.Context, key Keyable) (bool, error)

	// Block until refresh
	WaitForRefresh(ctx context.Context) error
}
