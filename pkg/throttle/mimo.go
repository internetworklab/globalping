package throttle

// MIMO (Multiple-Input, Multiple-Output) Throttles
// An MIMO remuxer/scheduler can be viewed as a combination of an MISO scheduler and a SIMO demuxer, perhaps plus some middlewares.

import (
	"context"
	"log"
	"sync"
)

type MIMOPacketizedItem struct {
	Label string
	Datum interface{}
}

// Multiple inputs, multiple outputs scheduler
type MIMOScheduler struct {
	config         *MIMOSchedConfig
	outputBindings sync.Map
	DefaultOutC    chan interface{}
}

type MIMOSchedConfig struct {
	Middlewares []SISOPipe
	Muxer       GenericMISOScheduler
}

func NewMIMOScheduler(config *MIMOSchedConfig) *MIMOScheduler {
	mimoSched := &MIMOScheduler{
		config:         config,
		DefaultOutC:    make(chan interface{}),
		outputBindings: sync.Map{},
	}
	return mimoSched
}

// AddInput adds a new labeled input channel to the source chain.
func (mimoSched *MIMOScheduler) AddInput(ctx context.Context, inputChan <-chan interface{}, label string) (opaqueNodeId interface{}, err error) {
	wrappedPktCh := make(chan interface{})
	go func() {
		defer close(wrappedPktCh)
		for pkt := range inputChan {
			wrappedPktCh <- MIMOPacketizedItem{Label: label, Datum: pkt}
		}
	}()
	return mimoSched.config.Muxer.AddInput(ctx, wrappedPktCh)
}

// Simply rely on source channel close to remove the input is un-reliable.
func (mimoSched *MIMOScheduler) RemoveInput(ctx context.Context, opaqueNodeId interface{}) error {
	return mimoSched.config.Muxer.RemoveInput(ctx, opaqueNodeId)
}

// AddOutput expresses an interest about a specific set of packets tagged with a specific label pattern.
// Note: outputChan should be buffered if you want to allow downstream processing to be slow.
func (mimoSched *MIMOScheduler) AddOutput(outputChan chan interface{}, label string) error {
	mimoSched.outputBindings.Store(label, outputChan)
	return nil
}

// RemoveOutput removes a specific labeled output binding.
func (mimoSched *MIMOScheduler) RemoveOutput(label string) error {
	mimoSched.outputBindings.Delete(label)
	return nil
}

// Run executes the scheduler, setting up the entire pipeline.
func (mimoSched *MIMOScheduler) Run(ctx context.Context) {

	var ch <-chan interface{} = mimoSched.config.Muxer.GetOutput()

	for _, middleware := range mimoSched.config.Middlewares {
		ch = WrapWithSISOPipe(ch, middleware)
	}

	go func() {
		defer close(mimoSched.DefaultOutC)

		for {
			select {
			case <-ctx.Done():
				return
			case packetraw, ok := <-ch:
				if !ok {
					return
				}
				log.Printf("[DBG] mimo sched received packet: %+v", packetraw)

				packet, ok := packetraw.(MIMOPacketizedItem)
				if !ok {
					panic("unexpected item type, it's not of a type of MIMOPacketizedItem")
				}

				nexthopUnknown, ok := mimoSched.outputBindings.Load(packet.Label)
				if ok {
					nexthop, ok := nexthopUnknown.(chan interface{})
					if !ok {
						panic("unexpected item type, it's not of a type of chan interface{}")
					}
					nexthop <- packet.Datum
					continue
				}

				log.Printf("[DBG] mimo sending packet to default output channel: %+v", packet.Datum)
				mimoSched.DefaultOutC <- packet.Datum
				log.Printf("[DBG] mimo sent packet to default output channel: %+v", packet.Datum)
			}
		}
	}()

}
