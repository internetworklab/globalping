package ratelimit

import (
	"context"
	"time"
)

type ServiceRequest struct {
	Func func(ctx context.Context) error
	Err  chan error
}

type MemoryBasedRateLimitPool struct {
	serviceChan     chan chan ServiceRequest
	tokensMap       map[string]int
	listeners       [](chan interface{})
	RefreshIntv     time.Duration
	NumTokensPerKey int
}

func (pool *MemoryBasedRateLimitPool) refresh() {
	pool.tokensMap = make(map[string]int)
}

func (pool *MemoryBasedRateLimitPool) doConsume(key string) int {
	if _, exists := pool.tokensMap[key]; !exists {
		pool.tokensMap[key] = pool.NumTokensPerKey
	}

	numTokens, _ := pool.tokensMap[key]
	numTokens = numTokens - 1
	pool.tokensMap[key] = numTokens
	return numTokens
}

func (pool *MemoryBasedRateLimitPool) Run(ctx context.Context) {
	pool.serviceChan = make(chan chan ServiceRequest)
	pool.refresh()

	go func(ctx context.Context) {
		defer close(pool.serviceChan)

		ticker := time.NewTicker(pool.RefreshIntv)
		defer ticker.Stop()

		for {
			serviceSubChan := make(chan ServiceRequest)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pool.refresh()
				for _, listener := range pool.listeners {
					close(listener)
				}
				pool.listeners = make([]chan interface{}, 0)
			case pool.serviceChan <- serviceSubChan:
				serviceRequest := <-serviceSubChan
				err := serviceRequest.Func(ctx)
				serviceRequest.Err <- err
			}
		}
	}(ctx)
}

func (pool *MemoryBasedRateLimitPool) WaitForRefresh(ctx context.Context) error {
	serviceCh, ok := <-pool.serviceChan
	if !ok {
		return context.Canceled
	}

	resultCh := make(chan chan interface{}, 1)

	serviceRequest := ServiceRequest{
		Func: func(ctx context.Context) error {
			listener := make(chan interface{})
			pool.listeners = append(pool.listeners, listener)
			resultCh <- listener
			return nil
		},
		Err: make(chan error),
	}
	serviceCh <- serviceRequest

	if err := <-serviceRequest.Err; err != nil {
		return err
	}
	listener := <-resultCh
	<-listener
	return nil
}

func (pool *MemoryBasedRateLimitPool) Consume(ctx context.Context, key Keyable) (bool, error) {
	serviceCh, ok := <-pool.serviceChan
	if !ok {
		return false, context.Canceled
	}

	resultCh := make(chan bool, 1)

	serviceRequest := ServiceRequest{
		Func: func(ctx context.Context) error {
			resultCh <- pool.doConsume(key.GetRatelimitKey()) >= 0
			return nil
		},
		Err: make(chan error),
	}
	serviceCh <- serviceRequest

	return <-resultCh, <-serviceRequest.Err
}

type MemoryBasedRateLimiter struct {
	Pool RateLimitPool
}

func (rl *MemoryBasedRateLimiter) GetIO(ctx context.Context, input chan Keyable) (chan interface{}, chan error) {
	outC := make(chan interface{})
	outErrC := make(chan error, 1)

	go func(ctx context.Context) {
		defer close(outC)
		defer close(outErrC)

		for {
			select {
			case <-ctx.Done():
				return
			case val, ok := <-input:
				if !ok {
					return
				}

				ok, err := rl.Pool.Consume(ctx, val)
				if err != nil {
					outErrC <- err
					return
				}

				if !ok {
					if err := rl.Pool.WaitForRefresh(ctx); err != nil {
						outErrC <- err
						return
					}
				}

				outC <- val
			}
		}
	}(ctx)

	return outC, outErrC
}
