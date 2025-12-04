package raw

// The sole purpose of this tracker package is to track the ICMP packets
// that has been sent, and generate the timeout events for the sent packets when the
// replies are still not received after running out of time.

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/net/icmp"
)

type ICMPTrackerEntry struct {
	SentAt     time.Time
	ReceivedAt []time.Time
	Timer      *time.Timer
}

func (itEnt *ICMPTrackerEntry) HasReceived() bool {
	if itEnt == nil {
		return false
	}
	return len(itEnt.ReceivedAt) > 0
}

func (itEnt *ICMPTrackerEntry) HasDup() bool {
	if itEnt == nil {
		return false
	}
	return len(itEnt.ReceivedAt) > 1
}

func (itEnt *ICMPTrackerEntry) RTTs() []time.Duration {
	if itEnt == nil {
		return nil
	}

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

const leastAcceptablePktIntervalMilliseconds = 10
const maximumAcceptablePktTimeoutSecs = 10

func estimateInternalTimeoutChCapacity(perPktTimeout time.Duration, pktInterval time.Duration) int {
	// According to the formula:
	// $$\text{Maximum Outstanding Packets} = \text{Maximum Send Rate} \times \text{Packet Timeout Duration}$$

	var redundancyFactor float64 = 1.5

	cap := int(redundancyFactor * float64(perPktTimeout.Milliseconds()) / float64(pktInterval.Milliseconds()))
	if cap < 1 {
		cap = 1
	}

	return cap
}

func NewICMPTracker(config *ICMPTrackerConfig) (*ICMPTracker, error) {
	if config.Interval.Milliseconds() < leastAcceptablePktIntervalMilliseconds {
		return nil, fmt.Errorf("interval must be at least %d milliseconds", leastAcceptablePktIntervalMilliseconds)
	}

	if config.PacketTimeout.Seconds() > maximumAcceptablePktTimeoutSecs {
		return nil, fmt.Errorf("packet timeout must be at most %d seconds", maximumAcceptablePktTimeoutSecs)
	}

	it := &ICMPTracker{
		id:              config.ID,
		initSeq:         config.InitialSeq,
		latestSeq:       config.InitialSeq,
		nrMaxCount:      config.MaxCount,
		store:           make(map[int]*ICMPTrackerEntry),
		serviceChan:     make(chan chan ServiceRequest),
		intv:            config.Interval,
		pktTimeout:      config.PacketTimeout,
		internTimeoutCh: make(chan int, estimateInternalTimeoutChCapacity(config.PacketTimeout, config.Interval)),
	}
	return it, nil
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
func (it *ICMPTracker) Run(ctx context.Context, conn *icmp.PacketConn) <-chan int {
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
	requestCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return nil
	}
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

			go func(seq int) {
				select {
				case it.internTimeoutCh <- seq:
					return
				case <-time.After(it.pktTimeout):
					return
				}
			}(seq)
		}
		return nil
	}

	select {
	case requestCh, ok := <-it.serviceChan:
		if !ok {
			// runner is closed
			return nil
		}
		defer close(requestCh)
		resultCh := make(chan error)
		defer close(resultCh)

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
	requestCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return nil
	}
	defer close(requestCh)

	fn := func(ctx context.Context) error {
		if ent, ok := it.store[seq]; ok {
			if ent.Timer != nil {
				ent.Timer.Stop()
				ent.Timer = nil
			}
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
	serviceCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return 0
	}
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
	requestCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return 0
	}
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
	requestCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return 0
	}
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
	requestCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return false
	}
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

func (it *ICMPTracker) ReadTrackerEntry(seq int) *ICMPTrackerEntry {
	serviceCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return nil
	}

	defer close(serviceCh)

	var result struct {
		entry *ICMPTrackerEntry
	}
	resultPtr := &result
	fn := func(ctx context.Context) error {
		if ent, ok := it.store[seq]; ok {
			newEnt := new(ICMPTrackerEntry)
			*newEnt = *ent
			newEnt.Timer = nil
			resultPtr.entry = newEnt
		}
		return nil
	}
	resultCh := make(chan error)
	serviceCh <- ServiceRequest{
		Func:   fn,
		Result: resultCh,
	}
	<-resultCh

	return resultPtr.entry
}

func (it *ICMPTracker) DeleteTrackerEntry(seq int) {
	serviceCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return
	}
	defer close(serviceCh)

	fn := func(ctx context.Context) error {
		delete(it.store, seq)
		return nil
	}

	resultCh := make(chan error)
	serviceCh <- ServiceRequest{
		Func:   fn,
		Result: resultCh,
	}
	<-resultCh
}
