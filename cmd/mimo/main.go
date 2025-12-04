package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	pkgratelimit "example.com/rbmq-demo/pkg/throttle"
)

func generateData(N int, prefix string) chan interface{} {
	dataChan := make(chan interface{})
	go func() {
		defer close(dataChan)
		for i := 0; i < N; i++ {
			dataChan <- fmt.Sprintf("%s%03d", prefix, i)
		}
	}()
	return dataChan
}

func main() {

	N := 100000
	c1 := generateData(N, "A")
	c2 := generateData(N, "B")
	c3 := generateData(N, "C")

	smootherCfg := pkgratelimit.BurstSmoother{
		LeastSampleInterval: 10 * time.Millisecond,
	}
	throttleCfg := pkgratelimit.TokenBasedThrottleConfig{
		RefreshInterval:       1 * time.Second,
		TokenQuotaPerInterval: 10,
	}
	mimoSched := pkgratelimit.NewMIMOScheduler(throttleCfg, smootherCfg)
	ctx := context.Background()
	ctx, cancelSched := context.WithCancel(ctx)
	defer cancelSched()
	mimoSched.Run(ctx)

	mimoSched.AddInput(c1, "A")
	mimoSched.AddInput(c2, "B")
	mimoSched.AddInput(c3, "C")

	o1 := make(chan interface{})
	o2 := make(chan interface{})

	mimoSched.AddOutput(o1, `(A|B)`, "o1")
	mimoSched.AddOutput(o2, `C`, "o2")

	meter1 := &pkgratelimit.SpeedMeasurer{
		RefreshInterval: 250 * time.Millisecond,
	}
	nullchan1, speedchan1 := meter1.Run(o1)
	meter2 := &pkgratelimit.SpeedMeasurer{
		RefreshInterval: 250 * time.Millisecond,
	}
	nullchan2, speedchan2 := meter2.Run(o2)

	wg := sync.WaitGroup{}
	wg.Add(2)

	ctx, cancelMeter := context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case itemC1, ok := <-nullchan1:
				if ok {
					log.Println("C1: sample", itemC1, time.Now().Format(time.RFC3339Nano))
				}
			case itemC2, ok := <-nullchan2:
				if ok {
					log.Println("C2: sample", itemC2, time.Now().Format(time.RFC3339Nano))
				}
			case record1, ok := <-speedchan1:
				if !ok {
					wg.Done()
					continue
				}
				log.Println("C1: speed", record1.String(), time.Now().Format(time.RFC3339Nano), "counter", record1.Counter)
			case record2, ok := <-speedchan2:
				if !ok {
					wg.Done()
					continue
				}
				log.Println("C2: speed", record2.String(), time.Now().Format(time.RFC3339Nano), "counter", record2.Counter)
			}
		}
	}()

	wg.Wait()
	log.Println("wg done")
	cancelMeter()
}
