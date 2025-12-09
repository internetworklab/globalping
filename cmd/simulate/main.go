package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgthrottle "example.com/rbmq-demo/pkg/throttle"
)

type SimplePacket struct {
	Class string
	Seq   int
}

func (sp SimplePacket) String() string {
	return fmt.Sprintf("SimplePacket{Class: %s, Seq: %d}", sp.Class, sp.Seq)
}

func generateData(N int, symbol string) chan interface{} {
	dataChan := make(chan interface{})
	go func() {
		defer close(dataChan)
		defer log.Printf("[DBG] generateData %s closed", symbol)
		for i := 0; i < N; i++ {
			dataChan <- SimplePacket{Class: symbol, Seq: i}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return dataChan
}

func runTestSession(
	ctx context.Context,
	hub *pkgthrottle.SharedThrottleHub,
	symbol string,
	sampleSize int,
) chan error {
	errCh := make(chan error, 1)

	sourceCh := generateData(sampleSize, symbol)

	go func() {
		defer func() {
			errCh <- nil
		}()

		proxySubCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		proxyCh, err := hub.CreateProxy(proxySubCtx, sourceCh)
		if err != nil {
			errCh <- err
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case item, ok := <-proxyCh:
				if !ok {
					return
				}
				pkt, ok := item.(SimplePacket)
				if !ok {
					panic("unexpected item type, it's not of a type of SimplePacket")
				}
				fmt.Printf("[%s] %s\n", time.Now().Format(time.RFC3339Nano), pkt)
				if pkt.Seq == sampleSize-1 {
					// last packet received, cleanup everything
					log.Printf("last packet of symbol %s received, cleanup everything", symbol)
					cancel()
				}
			}
		}
	}()

	return errCh
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	throttleConfig := pkgthrottle.TokenBasedThrottleConfig{
		RefreshInterval:       1 * time.Second,
		TokenQuotaPerInterval: 5,
	}

	tsSched, err := pkgthrottle.NewTimeSlicedEVLoopSched(&pkgthrottle.TimeSlicedEVLoopSchedConfig{})
	if err != nil {
		panic(fmt.Sprintf("failed to create time sliced event loop scheduler: %v", err))
	}
	tsSchedRunerr := tsSched.Run(ctx)

	throttle := pkgthrottle.NewTokenBasedThrottle(throttleConfig)
	throttle.Run()

	smoother := pkgthrottle.NewBurstSmoother(333 * time.Millisecond)
	smoother.Run()

	hub := pkgthrottle.NewICMPTransceiveHub(&pkgthrottle.SharedThrottleHubConfig{
		TSSched:  tsSched,
		Throttle: throttle,
		Smoother: smoother,
	})

	hub.Run(ctx)

	sampleSize := 20
	errChan1 := runTestSession(ctx, hub, "A", sampleSize)
	errChan2 := runTestSession(ctx, hub, "B", sampleSize)
	errChan3 := runTestSession(ctx, hub, "C", sampleSize)
	errChan4 := runTestSession(ctx, hub, "D", sampleSize)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	cancel()
	log.Printf("Received signal: %v, exiting...", sig)

	err = <-tsSchedRunerr
	if err != nil {
		log.Fatalf("failed to run time sliced event loop scheduler: %v", err)
	}

	log.Printf("waiting run sessions to finish ...")
	err = <-errChan1
	if err != nil {
		log.Fatalf("failed to run test session for symbol A: %v", err)
	}
	err = <-errChan2
	if err != nil {
		log.Fatalf("failed to run test session for symbol B: %v", err)
	}
	err = <-errChan3
	if err != nil {
		log.Fatalf("failed to run test session for symbol C: %v", err)
	}
	err = <-errChan4
	if err != nil {
		log.Fatalf("failed to run test session for symbol D: %v", err)
	}
}
