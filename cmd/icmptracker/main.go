package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgraw "example.com/rbmq-demo/pkg/raw"
)

type MockPing struct {
	Seq int
	RTT time.Duration
}

func (mp *MockPing) Run(tracker *pkgraw.ICMPTracker) {
	go func(seq int) {
		tracker.MarkSent(seq)
		log.Printf("Sent mock ping seq: %d", seq)
		<-time.After(mp.RTT)
		tracker.MarkReceived(seq)
		log.Printf("Send mock ping reply for seq %d", seq)
	}(mp.Seq)
}

func main() {
	trackerConfig := &pkgraw.ICMPTrackerConfig{
		PacketTimeout:                 3 * time.Second,
		TimeoutChannelEventBufferSize: 100,
	}
	tracker, err := pkgraw.NewICMPTracker(trackerConfig)
	if err != nil {
		log.Fatalf("failed to create ICMP tracker: %v", err)
	}

	ctx := context.TODO()
	tracker.Run(ctx)
	defer tracker.Close()

	go func() {
		log.Printf("Started listening for ICMPTracker events")
		for ev := range tracker.RecvEvC {
			evJsonB, _ := json.Marshal(ev)
			evJson := string(evJsonB)
			log.Printf("Received ICMPTracker event: %s, %s", evJson, time.Now().Format(time.RFC3339Nano))
		}
	}()

	go func() {
		// produce some mock pings and mock replies
		mockPings := []MockPing{
			{Seq: 1, RTT: 1000 * time.Millisecond},
			{Seq: 2, RTT: 1000 * time.Millisecond},
			{Seq: 3, RTT: 1000 * time.Millisecond},
			{Seq: 4, RTT: 3500 * time.Millisecond},
			{Seq: 5, RTT: 3500 * time.Millisecond},
			{Seq: 6, RTT: 1000 * time.Millisecond},
		}

		for _, mockPing := range mockPings {
			mockPing.Run(tracker)
			<-time.After(mockPing.RTT)
		}

	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("Received signal: %s, exiting...", sig.String())

}
