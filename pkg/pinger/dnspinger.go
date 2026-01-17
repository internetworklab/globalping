package pinger

import (
	"context"
	"sync"

	pkgdnsprobe "example.com/rbmq-demo/pkg/dnsprobe"
)

type DNSPinger struct {
	Requests []pkgdnsprobe.LookupParameter
}

func (dp *DNSPinger) Ping(ctx context.Context) <-chan PingEvent {
	evChan := make(chan PingEvent)
	go func() {
		defer close(evChan)
		wg := &sync.WaitGroup{}
		defer wg.Wait()
		for _, request := range dp.Requests {
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
