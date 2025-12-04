package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgthrottle "example.com/rbmq-demo/pkg/throttle"
)

func main() {
	throttleConfig := pkgthrottle.TokenBasedThrottleConfig{
		RefreshInterval:       1 * time.Second,
		TokenQuotaPerInterval: 20,
	}
	smootherConfig := pkgthrottle.BurstSmoother{
		LeastSampleInterval: 100 * time.Millisecond,
	}
	mimoScheduler := pkgthrottle.NewMIMOScheduler(throttleConfig, smootherConfig)

	icmpHub := pkgthrottle.NewICMPTransceiveHub(&pkgthrottle.SharedThrottleHubConfig{
		MIMOScheduler: mimoScheduler,
	})
	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mimoScheduler.Run(ctx)

	icmpHub.Run(ctx)

	hubProxy1 := icmpHub.GetProxy()
	if hubProxy1 == nil {
		// the hub was shutdown pre-maturely
		return
	}
	hubProxy2 := icmpHub.GetProxy()
	if hubProxy2 == nil {
		// the hub was shutdown pre-maturely
		return
	}

	// fake death ping generator
	go func() {
		defer log.Println("[DBG] generator 1 closed")
		defer hubProxy1.Close()

		log.Println("[DBG] generator 1 started")

		writeCh := hubProxy1.GetWriter()

		// try generating ping requests at death speed
		lastSeq := 0

		for {
			mockPacket := pkgthrottle.TestPacket{
				Host: "www.example.com",
				Type: pkgthrottle.TestPacketTypePing,
				Seq:  lastSeq,
				Id:   1,
			}
			select {
			case <-ctx.Done():
				return
			case writeCh <- mockPacket:
				lastSeq++
			}
		}
	}()

	// second death ping generator
	go func() {
		defer log.Println("[DBG] generator 2 closed")
		defer hubProxy2.Close()

		log.Println("[DBG] generator 2 started")
		lastSeq := 0
		writeCh := hubProxy2.GetWriter()

		for {
			mockPacket := pkgthrottle.TestPacket{
				Host: "x.com",
				Type: pkgthrottle.TestPacketTypePing,
				Seq:  lastSeq,
				Id:   1,
			}
			select {
			case <-ctx.Done():
				return
			case writeCh <- mockPacket:
				lastSeq++
			}
		}

	}()

	// fake death pong receiver
	go func() {
		defer log.Println("[DBG] receiver 1 closed")
		log.Println("[DBG] receiver 1 started")

		speedMeter := pkgthrottle.SpeedMeasurer{
			RefreshInterval: 250 * time.Millisecond,
			MinTimeDelta:    250 * time.Millisecond,
		}

		readCh := hubProxy1.GetReader()
		readCh, speed := speedMeter.Run(readCh)
		go func() {
			for speedRecord := range speed {
				log.Printf("[DBG] receiver 1 Speed: %s", speedRecord.String())
			}
		}()
		for pong := range readCh {
			if pongPkt, ok := pong.(pkgthrottle.TestPacket); ok && pongPkt.Type == pkgthrottle.TestPacketTypePong {
				// log.Printf("[DBG] receiver 1 Received pong: %+v", pongPkt)
			}
		}
	}()

	// fake death pong receiver
	go func() {
		defer log.Println("[DBG] receiver 2 closed")
		log.Println("[DBG] receiver 2 started")

		speedMeter := pkgthrottle.SpeedMeasurer{
			RefreshInterval: 250 * time.Millisecond,
			MinTimeDelta:    250 * time.Millisecond,
		}

		readCh := hubProxy2.GetReader()
		readCh, speed := speedMeter.Run(readCh)
		go func() {
			for speedRecord := range speed {
				log.Printf("[DBG] receiver 2 Speed: %s", speedRecord.String())
			}
		}()
		for pong := range readCh {
			if pongPkt, ok := pong.(pkgthrottle.TestPacket); ok && pongPkt.Type == pkgthrottle.TestPacketTypePong {
				// log.Printf("[DBG] receiver 2 Received pong: %+v", pongPkt)
			}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	log.Printf("Received signal: %v, exiting...", sig)
}
