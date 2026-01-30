package ratelimit

import (
	"context"
)

type Keyable interface {
	GetRatelimitKey() string
}

type RateLimiter interface {
	// returns an input channel, an output channel, and an error channel
	// the output channel and the error channel will be closed when 
	// the input channel is closed, or when the context is done.
	GetIO(ctx context.Context) (chan<- interface{}, <-chan interface{}, chan error)
}

type RateLimitPool interface {
	// returns false when quota is exhausted, true otherwise
	// the second return value is error, if any, such as, when timeout occurs
	Consume(ctx context.Context, key string) (bool, error)

	// Block until refresh
	WaitForRefresh(ctx context.Context) error
}
