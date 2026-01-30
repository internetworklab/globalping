package ratelimit

import (
	"context"
	"fmt"
)

type KeyFunc func(ctx context.Context, object interface{}) (string, error)

func GlobalKeyFunc(ctx context.Context, object interface{}) (string, error) {
	return "_", nil
}

type MemoryBasedRateLimiter struct {
	Pool   RateLimitPool
	GetKey KeyFunc
}

func (rl *MemoryBasedRateLimiter) GetIO(ctx context.Context) (chan<- interface{}, <-chan interface{}, chan error) {
	inC := make(chan interface{})
	outC := make(chan interface{})
	outErrC := make(chan error, 1)

	go func(ctx context.Context) {
		defer close(outC)
		defer close(outErrC)

		for {
			select {
			case <-ctx.Done():
				return
			case val, ok := <-inC:
				if !ok {
					return
				}

				key, err := rl.GetKey(ctx, val)
				if err != nil {
					outErrC <- fmt.Errorf("failed to get key: %w", err)
					return
				}

				ok, err = rl.Pool.Consume(ctx, key)
				if err != nil {
					outErrC <- err
					return
				}

				if !ok {
					for {
						err := rl.Pool.WaitForRefresh(ctx)
						if err != nil {
							outErrC <- err
							return
						}

						ok, err = rl.Pool.Consume(ctx, key)
						if err != nil {
							outErrC <- err
							return
						}

						if ok {
							break
						}
					}
				}

				outC <- val
			}
		}
	}(ctx)

	return inC, outC, outErrC
}
