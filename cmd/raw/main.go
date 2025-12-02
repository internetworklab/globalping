package main

import (
	"context"
	"log"
	"net"
	"os"
	"runtime"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type ICMPTrackerEntry struct {
	SentAt     time.Time
	ReceivedAt []time.Time
	Timer      *time.Timer
}

func (itEnt *ICMPTrackerEntry) HasReceived() bool {
	return len(itEnt.ReceivedAt) > 0
}

func (itEnt *ICMPTrackerEntry) HasDup() bool {
	return len(itEnt.ReceivedAt) > 1
}

func (itEnt *ICMPTrackerEntry) RTT() []time.Duration {
	deltas := make([]time.Duration, 0)
	for _, receivedAt := range itEnt.ReceivedAt {
		deltas = append(deltas, receivedAt.Sub(itEnt.SentAt))
	}
	return deltas
}

type ServiceRequest struct {
	Func   func(ctx context.Context) error
	Result chan error
}

type ICMPTracker struct {
	id              int
	initSeq         int
	latestSeq       int
	store           map[int]*ICMPTrackerEntry
	serviceChan     chan chan ServiceRequest
	closeCh         chan interface{}
	nrUnAck         int
	nrMaxCount      *int
	intv            time.Duration
	pktTimeout      time.Duration
	internTimeoutCh chan int
}

type ICMPTrackerConfig struct {
	ID            int
	InitialSeq    int
	MaxCount      *int
	PacketTimeout time.Duration
	Interval      time.Duration
}

func NewICMPTracker(config *ICMPTrackerConfig) *ICMPTracker {
	it := &ICMPTracker{
		id:          config.ID,
		initSeq:     config.InitialSeq,
		latestSeq:   config.InitialSeq,
		nrMaxCount:  config.MaxCount,
		store:       make(map[int]*ICMPTrackerEntry),
		serviceChan: make(chan chan ServiceRequest),
		intv:        config.Interval,
		pktTimeout:  config.PacketTimeout,
	}
	return it
}

func (it *ICMPTracker) doRun(ctx context.Context) {
	it.closeCh = make(chan interface{})
	defer close(it.serviceChan)

	for {

		serviceSubCh := make(chan ServiceRequest)

		select {
		case <-it.closeCh:
			return
		case it.serviceChan <- serviceSubCh:
			serviceReq := <-serviceSubCh
			err := serviceReq.Func(ctx)
			serviceReq.Result <- err
			close(serviceReq.Result)
		}
	}
}

// returns a read-only channel of timeout events
// and this timeout chan must be consumed to avoid deadlock.
func (it *ICMPTracker) Run(ctx context.Context) <-chan int {
	go it.doRun(ctx)

	timeoutCh := make(chan int)
	go func() {
		defer close(timeoutCh)
		for seq := range it.internTimeoutCh {
			timeoutCh <- seq
		}
	}()

	return timeoutCh
}

func (it *ICMPTracker) MarkSent(seq int) error {
	requestCh := <-it.serviceChan
	defer close(requestCh)

	fn := func(ctx context.Context) error {

		ent := &ICMPTrackerEntry{
			SentAt: time.Now(),
			Timer:  time.NewTimer(it.pktTimeout),
		}
		it.store[seq] = ent
		it.nrUnAck++

		go func() {
			<-ent.Timer.C
			ctx, cancel := context.WithTimeout(context.TODO(), it.pktTimeout)
			defer cancel()
			it.tryMarkAsTimeout(ctx, seq)
		}()

		return nil
	}

	resultCh := make(chan error)
	requestCh <- ServiceRequest{
		Func:   fn,
		Result: resultCh,
	}

	return <-resultCh
}

func (it *ICMPTracker) tryMarkAsTimeout(ctx context.Context, seq int) error {

	fn := func(ctx context.Context) error {
		if ent, ok := it.store[seq]; ok {
			if len(ent.ReceivedAt) > 0 {
				return nil
			}

			it.nrUnAck--
			var zeroTime time.Time
			ent.ReceivedAt = append(ent.ReceivedAt, zeroTime)
			it.internTimeoutCh <- seq
		}
		return nil
	}
	resultCh := make(chan error)

	select {
	case requestCh, ok := <-it.serviceChan:
		if !ok {
			// runner is closed
			return nil
		}
		defer close(requestCh)

		requestCh <- ServiceRequest{
			Func:   fn,
			Result: resultCh,
		}
		return <-resultCh
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (it *ICMPTracker) MarkReceived(seq int) error {
	requestCh := <-it.serviceChan
	defer close(requestCh)

	fn := func(ctx context.Context) error {
		if ent, ok := it.store[seq]; ok {
			if ent.ReceivedAt == nil {
				it.nrUnAck--
			}
			ent.ReceivedAt = append(ent.ReceivedAt, time.Now())
		}
		return nil
	}

	resultCh := make(chan error)

	requestCh <- ServiceRequest{
		Func:   fn,
		Result: resultCh,
	}

	return <-resultCh
}

func (it *ICMPTracker) GetNrUnAck() int {
	var nrUnAck *int = new(int)
	serviceCh := <-it.serviceChan
	defer close(serviceCh)

	fn := func(ctx context.Context) error {
		*nrUnAck = it.nrUnAck
		return nil
	}
	resultCh := make(chan error)
	serviceCh <- ServiceRequest{
		Func:   fn,
		Result: resultCh,
	}
	<-resultCh
	return *nrUnAck
}

func (it *ICMPTracker) IterateSeq() int {
	requestCh := <-it.serviceChan
	defer close(requestCh)

	var seqPtr *int = new(int)

	fn := func(ctx context.Context) error {
		seq := it.latestSeq
		it.latestSeq++
		*seqPtr = seq
		return nil
	}

	resultCh := make(chan error)
	requestCh <- ServiceRequest{
		Func:   fn,
		Result: resultCh,
	}
	<-resultCh
	return *seqPtr
}

func shouldICMPTrackerContinue(latestSeq, initSeq, maxCount int) bool {
	return (latestSeq - initSeq) < maxCount
}

func (it *ICMPTracker) WaitForNext() time.Duration {
	requestCh := <-it.serviceChan
	defer close(requestCh)

	noSleep := time.Duration(0)
	var sleepDuration *time.Duration = &noSleep

	fn := func(ctx context.Context) error {
		// don't actually sleep here, just determine the sleep duration
		if it.nrMaxCount != nil {
			if shouldICMPTrackerContinue(it.latestSeq, it.initSeq, *it.nrMaxCount) {
				// sleep only if there's still more to send
				*sleepDuration = it.intv
			}
		} else {
			// there's always more to send
			*sleepDuration = it.intv
		}
		return nil
	}
	resultCh := make(chan error)
	requestCh <- ServiceRequest{
		Func:   fn,
		Result: resultCh,
	}
	<-resultCh
	return *sleepDuration
}

func (it *ICMPTracker) IsNotDone() bool {
	requestCh := <-it.serviceChan
	defer close(requestCh)

	var contPtr *bool = new(bool)
	fn := func(ctx context.Context) error {
		var cont bool
		if it.nrMaxCount != nil {
			cont = shouldICMPTrackerContinue(it.latestSeq, it.initSeq, *it.nrMaxCount)
		} else {
			cont = true
		}
		*contPtr = cont
		return nil
	}
	resultCh := make(chan error)
	requestCh <- ServiceRequest{
		Func:   fn,
		Result: resultCh,
	}
	<-resultCh
	return *contPtr
}

func (it *ICMPTracker) Close() {
	if it.closeCh == nil || it.internTimeoutCh == nil {
		panic("ICMPTracker is not started yet")
	}
	close(it.closeCh)
	close(it.internTimeoutCh)
}

func (it *ICMPTracker) GetID() int {
	return it.id
}

const standardMTU = 1500

func main() {
	if runtime.GOOS != "linux" {
		log.Fatal("This program only runs on Linux")
	}

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		log.Fatalf("failed to listen on packet:icmp: %v", err)
	}

	defer conn.Close()

	icmpID := os.Getpid() & 0xffff
	maxCount := 10
	pktTimeout := 3 * time.Second
	icmpTrackerConfig := &ICMPTrackerConfig{
		ID:            icmpID,
		InitialSeq:    0,
		MaxCount:      &maxCount,
		PacketTimeout: pktTimeout,
		Interval:      500 * time.Millisecond,
	}
	tracker := NewICMPTracker(icmpTrackerConfig)

	ctx := context.TODO()
	timeoutCh := tracker.Run(ctx)
	go func() {
		for seq := range timeoutCh {
			log.Printf("timeout for seq: %v", seq)
		}
	}()
	defer tracker.Close()

	receiveCh := make(chan interface{})
	go func() {
		defer close(receiveCh)

		for tracker.IsNotDone() || tracker.GetNrUnAck() > 0 {

			if err := conn.SetReadDeadline(time.Now().Add(pktTimeout)); err != nil {
				log.Fatalf("failed to set read deadline: %v", err)
			}

			receivBuf := make([]byte, standardMTU)
			n, peer, err := conn.ReadFrom(receivBuf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					log.Printf("timeout reading from connection, skipping")
					continue
				}

				log.Fatalf("failed to read from connection: %v", err)
			}
			log.Printf("read %d bytes from %v", n, peer.String())

			receivMsg, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), receivBuf[:n])
			if err != nil {
				log.Fatalf("failed to parse icmp message: %v", err)
			}

			if icmpEcho, ok := receivMsg.Body.(*icmp.Echo); ok {
				if icmpEcho.ID == tracker.GetID() {
					tracker.MarkReceived(icmpEcho.Seq)
					log.Printf("id: %v, seq: %v", icmpEcho.ID, icmpEcho.Seq)
				}
			}
		}

	}()

	for tracker.IsNotDone() {
		seq := tracker.IterateSeq()
		writeMsg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Body: &icmp.Echo{
				ID:  icmpID,
				Seq: seq,
			},
		}
		writeBuff, err := writeMsg.Marshal(nil)
		if err != nil {
			log.Fatalf("failed to marshal icmp message: %v", err)
		}

		if _, err := conn.WriteTo(writeBuff, &net.IPAddr{IP: net.ParseIP("8.8.4.4")}); err != nil {
			log.Fatalf("failed to write to connection: %v", err)
		}
		tracker.MarkSent(seq)
		if sleepDur := tracker.WaitForNext(); sleepDur > 0 {
			time.Sleep(sleepDur)
		}
	}

	<-receiveCh
}
