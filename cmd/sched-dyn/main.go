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

func anonymousSource(ctx context.Context, symbol int, limit *int) chan interface{} {
	outC := make(chan interface{})
	numItemsCopied := 0
	go func() {
		// close source channel to signal the down stream consumers
		defer close(outC)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				outC <- symbol
				numItemsCopied++
				if limit != nil && *limit > 0 && numItemsCopied >= *limit {
					log.Printf("source %d reached limit %d, stopping", symbol, *limit)
					return
				}
			}
		}
	}()
	return outC
}

func add(ctx context.Context, symbol int, limit *int, evCenter *pkgthrottle.TimeSlicedEVLoopSched) interface{} {

	dataSource := anonymousSource(ctx, symbol, limit)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	opaqueNodeId, err := evCenter.AddInput(ctx, dataSource)
	if err != nil {
		log.Fatalf("failed to add input to evCenter: %v", err)
	}
	return opaqueNodeId
}

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	evCenter, err := pkgthrottle.NewTimeSlicedEVLoopSched(&pkgthrottle.TimeSlicedEVLoopSchedConfig{})
	if err != nil {
		log.Fatalf("failed to create evCenter: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errorChan := evCenter.Run(ctx)

	outC := evCenter.GetOutput()

	var nodesCount *int = new(int)
	*nodesCount = 0

	var numEventsPassed *int = new(int)
	*numEventsPassed = 0

	aLim := 80000
	bLim := 160000
	cLim := 240000

	// consumer goroutine
	go func() {
		stat := make(map[int]int)
		var total *int = new(int)

		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				fmt.Println("Total: ", *total)
			}
		}()

		for muxedItem := range outC {
			stat[muxedItem.(int)]++
			*total = *total + 1
			if *total%1000 == 0 {
				for k, v := range stat {
					fmt.Printf("%d: %d, %.2f%%\n", k, v, 100*float64(v)/float64(*total))
				}
			}
			if *total == aLim+bLim+cLim {
				fmt.Println("Final statistics:")
				for k, v := range stat {
					fmt.Printf("%d: %d, %.2f%%\n", k, v, 100*float64(v)/float64(*total))
				}
			}
		}

	}()

	evCenter.RegisterCustomEVHandler(ctx, pkgthrottle.TSSchedEVNodeDrained, "node_drained", func(evObj *pkgthrottle.TSSchedEVObject) error {

		nodeId, ok := evObj.Payload.(int)
		if !ok {
			panic("unexpected ev payload, it's not of a type of int")
		}

		log.Printf("node %d is drained", nodeId)

		evObj.Result <- nil
		return nil
	})

	opaqueNodeId := add(ctx, 1, &aLim, evCenter)
	log.Printf("node %v is added", opaqueNodeId)
	opaqueNodeId = add(ctx, 2, &bLim, evCenter)
	log.Printf("node %v is added", opaqueNodeId)
	opaqueNodeId = add(ctx, 3, &cLim, evCenter)
	log.Printf("node %v is added", opaqueNodeId)

	sig := <-sigs
	fmt.Println("signal received: ", sig, " exitting...")

	cancel()

	err, ok := <-errorChan
	if ok && err != nil {
		log.Fatalf("event loop error: %v", err)
	}
}
