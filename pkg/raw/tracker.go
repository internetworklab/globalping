package raw

// The sole purpose of this tracker package is to track the ICMP packets
// that has been sent, and generate the timeout events for the sent packets when the
// replies are still not received after running out of time.

import (
	"context"
	"fmt"
	"log"
	"time"
)

type ICMPTrackerEntry struct {
	Seq        int
	SentAt     time.Time
	ReceivedAt []time.Time
	Timer      *time.Timer `json:"-"`
	Raw        []interface{}
}

func (itEnt *ICMPTrackerEntry) ReadonlyClone() *ICMPTrackerEntry {
	if itEnt == nil {
		return nil
	}

	newOne := new(ICMPTrackerEntry)
	*newOne = *itEnt
	newOne.Timer = nil
	newOne.ReceivedAt = make([]time.Time, len(itEnt.ReceivedAt))
	copy(newOne.ReceivedAt, itEnt.ReceivedAt)
	newOne.Raw = make([]interface{}, len(itEnt.Raw))
	copy(newOne.Raw, itEnt.Raw)
	return newOne
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
	store       map[int]*ICMPTrackerEntry
	serviceChan chan chan ServiceRequest
	pktTimeout  time.Duration

	// Receiving Events
	// A empty array of ReceivedAt means timeout
	RecvEvC chan ICMPTrackerEntry
}

type ICMPTrackerConfig struct {
	PacketTimeout                 time.Duration
	TimeoutChannelEventBufferSize int
}

func NewICMPTracker(config *ICMPTrackerConfig) (*ICMPTracker, error) {
	it := &ICMPTracker{
		store:       make(map[int]*ICMPTrackerEntry),
		serviceChan: make(chan chan ServiceRequest),
		RecvEvC:     make(chan ICMPTrackerEntry, config.TimeoutChannelEventBufferSize),
		pktTimeout:  config.PacketTimeout,
	}
	return it, nil
}

// returns a read-only channel of timeout events
func (it *ICMPTracker) Run(ctx context.Context) {
	go func() {
		defer close(it.serviceChan)

		for {
			serviceSubCh := make(chan ServiceRequest)

			select {
			case <-ctx.Done():
				return
			case it.serviceChan <- serviceSubCh:
				serviceReq := <-serviceSubCh
				err := serviceReq.Func(ctx)
				serviceReq.Result <- err
				close(serviceReq.Result)
			}
		}
	}()
}

func (it *ICMPTracker) cleanupEntry(seq int) {
	// log.Printf("[DBG] clean up outdated entry for seq: %d, store: %+v", seq, it.store)
	requestCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return
	}
	defer close(requestCh)

	fn := func(ctx context.Context) error {
		delete(it.store, seq)
		return nil
	}
	req := ServiceRequest{
		Func:   fn,
		Result: make(chan error),
	}
	requestCh <- req

	// log.Printf("[DBG] outdated entry for seq %d has been cleaned: store: %+v", seq, it.store)

	err := <-req.Result
	if err != nil {
		log.Printf("failed to cleanup entry for seq %d: %v", seq, err)
	}
}

func (it *ICMPTracker) handleTimeout(seq int) {
	requestCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return
	}
	defer close(requestCh)

	fn := func(ctx context.Context) error {
		if ent, ok := it.store[seq]; ok {
			if len(ent.ReceivedAt) > 0 {
				return nil
			}
			if clone := ent.ReadonlyClone(); clone != nil {
				go func(ent ICMPTrackerEntry) {
					it.RecvEvC <- ent
				}(*clone)
			}
		}
		delete(it.store, seq)
		return nil
	}

	req := ServiceRequest{
		Func:   fn,
		Result: make(chan error),
	}
	requestCh <- req
	err := <-req.Result
	if err != nil {
		log.Printf("failed to handle timeout for seq %d: %v", seq, err)
	}
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
			Seq:    seq,
			SentAt: time.Now(),
			Timer:  time.NewTimer(it.pktTimeout),
		}
		it.store[seq] = ent

		go func() {
			<-ent.Timer.C
			it.handleTimeout(seq)
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

func (it *ICMPTracker) MarkReceived(seq int, raw interface{}) error {
	requestCh, ok := <-it.serviceChan
	if !ok {
		// engine is already shutdown
		return fmt.Errorf("engine is closed")
	}
	defer close(requestCh)

	fn := func(ctx context.Context) error {
		if ent, ok := it.store[seq]; ok {
			if ent.Timer != nil {
				ent.Timer.Stop()
				ent.Timer = nil
			}
			ent.ReceivedAt = append(ent.ReceivedAt, time.Now())
			ent.Raw = append(ent.Raw, raw)
			if clone := ent.ReadonlyClone(); clone != nil {
				go func(ent ICMPTrackerEntry) {
					it.RecvEvC <- ent
				}(*clone)
			}

			go func() {
				// we won't keep the entry indefinitely just for waiting dup icmp replies.
				<-time.After(it.pktTimeout)
				it.cleanupEntry(seq)
			}()
		}
		return nil
	}

	req := ServiceRequest{
		Func:   fn,
		Result: make(chan error),
	}
	requestCh <- req
	err := <-req.Result
	if err != nil {
		return fmt.Errorf("failed to handle in-time for seq %d: %v", seq, err)
	}
	return nil
}
