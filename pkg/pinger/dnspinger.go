package pinger

import (
	"context"
	"sync"

	pkgdnsprobe "example.com/rbmq-demo/pkg/dnsprobe"
	pkgratelimit "example.com/rbmq-demo/pkg/ratelimit"
)

type DNSPinger struct {
	Requests    []pkgdnsprobe.LookupParameter
	RateLimiter pkgratelimit.RateLimiter
}

func getRequests(ctx context.Context, requests []pkgdnsprobe.LookupParameter, ratelimiter pkgratelimit.RateLimiter) chan pkgdnsprobe.LookupParameter {
	requestChan := make(chan pkgdnsprobe.LookupParameter)

	go func(ctx context.Context) {
		defer close(requestChan)
		if ratelimiter == nil {
			for _, req := range requests {
				requestChan <- req
			}
			return
		}

		inC, outC, _ := ratelimiter.GetIO(ctx)
		go func(ctx context.Context) {
			defer close(inC)
			for _, req := range requests {
				inC <- req
			}
		}(ctx)

		for req := range outC {
			requestChan <- req.(pkgdnsprobe.LookupParameter)
		}
	}(ctx)
	return requestChan
}

func (dp *DNSPinger) Ping(ctx context.Context) <-chan PingEvent {
	evChan := make(chan PingEvent)
	go func() {
		defer close(evChan)
		wg := &sync.WaitGroup{}
		defer wg.Wait()
		for request := range getRequests(ctx, dp.Requests, dp.RateLimiter) {
			wg.Add(1)
			go func(req pkgdnsprobe.LookupParameter) {
				defer wg.Done()
				queryResult, err := pkgdnsprobe.LookupDNS(ctx, req)
				if err != nil {
					evChan <- PingEvent{Error: err}
					return
				}
				queryResult, err = queryResult.PreStringify()
				if err != nil {
					evChan <- PingEvent{Error: err}
					return
				}
				evChan <- PingEvent{Data: queryResult}
			}(request)
		}
	}()
	return evChan
}
