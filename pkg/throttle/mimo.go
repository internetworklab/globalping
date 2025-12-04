package throttle

// MIMO (Multiple-Input, Multiple-Output) Throttles

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sync"
)

const (
	maxNrSources int = 1000
)

// MIMOInputSourceNode represents a node in the single-linked list of input channels.
type MIMOInputSourceNode struct {
	InputChan chan interface{}
	Label     string
	NextNode  *MIMOInputSourceNode
	Dead      bool
}

// LabeledPacketBinding defines a routing rule for demultiplexing.
type LabeledPacketBinding struct {
	BindingName  string
	LabelPattern string
	Pattern      *regexp.Regexp
	OutputChan   chan<- interface{}
}

// wrappedpacket encapsulates the original packet with its source label.
type wrappedpacket struct {
	label  string
	packet interface{}
}

type EventType string

const (
	EVSourceAdded     EventType = "source_added"
	EVSourceDrained   EventType = "source_drained"
	EVNewPktAvailable EventType = "new_pkt_available"
	EVNewOutputAdded  EventType = "new_output"
	EVOutputRemoved   EventType = "output_removed"
)

type EventResult struct {
	Error error
}

type EventObject struct {
	Type EventType

	// possible payload, depends on the concrete event type
	// the requester sends this to the hub
	Payload interface{}

	// possible result, the hub sends this to the requester
	Result chan *EventResult
}

// Multiple inputs, multiple outputs scheduler
type MIMOScheduler struct {
	// Muxed data channel (Input of the Throttle)
	// For interoperability, we use interface{} here instead of wrappedpacket
	// so that it can pass nearly any type of transformer middlewares
	muxedDataChan chan interface{}
	// The head of the linked list of input sources
	sourceChain *MIMOInputSourceNode

	outputBindings sync.Map

	// Throttle components configuration
	ThrottleConfig TokenBasedThrottleConfig
	SmootherConfig BurstSmoother

	muxerServiceChan chan chan EventObject
	nrInputs         int
}

func NewMIMOScheduler(throttleCfg TokenBasedThrottleConfig, smootherCfg BurstSmoother) *MIMOScheduler {
	mimoSched := &MIMOScheduler{
		muxedDataChan:    make(chan interface{}, throttleCfg.TokenQuotaPerInterval),
		ThrottleConfig:   throttleCfg,
		SmootherConfig:   smootherCfg,
		muxerServiceChan: make(chan chan EventObject),
	}
	return mimoSched
}

// returns:
// (the packets collected, the node stopped)
func (sched *MIMOScheduler) collectWrappedPktFromLink(head *MIMOInputSourceNode) (*wrappedpacket, *MIMOInputSourceNode) {
	if head == nil {
		return nil, nil
	}

	currentNext := head

	for {
		select {
		case pkt, ok := <-currentNext.InputChan:
			if !ok {
				currentNext.Dead = true

				// go notify the hub that there is some dead source(s) need to be removed
				go func() {
					evCh := <-sched.muxerServiceChan
					defer close(evCh)
					evObj := EventObject{
						Type:    EVSourceDrained,
						Payload: nil,
						Result:  make(chan *EventResult),
					}
					evCh <- evObj
					<-evObj.Result
				}()
			} else {
				return &wrappedpacket{label: currentNext.Label, packet: pkt}, currentNext.NextNode
			}
		default:
			// noop, simply for making the operation non-blocking
		}

		currentNext = currentNext.NextNode
		if currentNext == head {
			break
		}
	}
	return nil, nil
}

func insertNode(head *MIMOInputSourceNode, newNode *MIMOInputSourceNode) *MIMOInputSourceNode {
	if head == nil {
		newNode.NextNode = newNode
		return newNode
	}

	newNode.NextNode = head.NextNode
	head.NextNode = newNode

	return head
}

func removeDeadNodes(head *MIMOInputSourceNode) *MIMOInputSourceNode {
	if head == nil {
		return head
	}

	var newList *MIMOInputSourceNode
	originalHead := head
	for {
		if head.Dead {
			continue
		}
		newList = insertNode(newList, head)

		head = head.NextNode
		if head == originalHead {
			break
		}
	}

	return newList
}

// AddInput adds a new labeled input channel to the source chain.
func (mimoSched *MIMOScheduler) AddInput(inputChan <-chan interface{}, label string) error {
	requestCh, ok := <-mimoSched.muxerServiceChan
	if !ok {
		// the engine was shutdown
		return fmt.Errorf("engine was shutdown")
	}

	defer close(requestCh)
	newNode := &MIMOInputSourceNode{
		InputChan: make(chan interface{}, 1),
		Label:     label,
	}

	evObj := EventObject{
		Type:    EVSourceAdded,
		Payload: newNode,
		Result:  make(chan *EventResult),
	}
	requestCh <- evObj
	res := <-evObj.Result

	go func() {
		for pkt := range inputChan {
			newNode.InputChan <- pkt
			// go notify the hub that there is some new packet available
			go func() {
				evCh, ok := <-mimoSched.muxerServiceChan
				if !ok {
					// the engine was shutdown
					return
				}
				defer close(evCh)
				evObj := EventObject{
					Type: EVNewPktAvailable,
					// the hub will scan the whole list for any data that is available
					Payload: nil,
					Result:  make(chan *EventResult),
				}
				evCh <- evObj
				<-evObj.Result
			}()
		}
	}()

	return res.Error
}

// AddOutput expresses an interest about a specific set of packets tagged with a specific label pattern.
// Note: outputChan should be buffered if you want to allow downstream processing to be slow.
func (mimoSched *MIMOScheduler) AddOutput(outputChan chan<- interface{}, labelPattern string, labelBindingName string) error {
	re, err := regexp.Compile(labelPattern)
	if err != nil {
		return fmt.Errorf("invalid label pattern %q: %w", labelPattern, err)
	}

	binding := &LabeledPacketBinding{
		BindingName:  labelBindingName,
		LabelPattern: labelPattern,
		Pattern:      re,
		OutputChan:   outputChan,
	}

	evCh, ok := <-mimoSched.muxerServiceChan
	if !ok {
		// the engine was shutdown
		return fmt.Errorf("engine was shutdown")
	}
	defer close(evCh)

	evObj := EventObject{
		Type:    EVNewOutputAdded,
		Payload: binding,
		Result:  make(chan *EventResult),
	}
	evCh <- evObj
	res := <-evObj.Result

	return res.Error
}

// RemoveOutput removes a specific labeled output binding.
func (mimoSched *MIMOScheduler) RemoveOutput(labelBindingName string) error {
	evCh, ok := <-mimoSched.muxerServiceChan
	if !ok {
		// the engine was shutdown
		return fmt.Errorf("engine was shutdown")
	}
	defer close(evCh)
	evObj := EventObject{
		Type:    EVOutputRemoved,
		Payload: labelBindingName,
		Result:  make(chan *EventResult),
	}
	evCh <- evObj
	res := <-evObj.Result
	return res.Error
}

func (evObj *EventObject) handleNewInputAdded(sched *MIMOScheduler) error {
	if sched.nrInputs >= maxNrSources {
		return fmt.Errorf("maximum number of inputs reached")
	}

	newNode := evObj.Payload.(*MIMOInputSourceNode)
	sched.sourceChain = insertNode(sched.sourceChain, newNode)

	return nil
}

func (evObj *EventObject) handleSourceDrained(sched *MIMOScheduler) error {
	sched.sourceChain = removeDeadNodes(sched.sourceChain)
	return nil
}

func (evObj *EventObject) handleNewPktAvailable(sched *MIMOScheduler) error {
	wrappedPkt, lastNode := sched.collectWrappedPktFromLink(sched.sourceChain)
	sched.sourceChain = lastNode

	if wrappedPkt != nil {
		sched.muxedDataChan <- *wrappedPkt
	}
	sched.sourceChain = lastNode

	return nil
}

func (evObj *EventObject) handleNewOutputAdded(sched *MIMOScheduler) error {
	binding := evObj.Payload.(*LabeledPacketBinding)
	sched.outputBindings.Store(binding.BindingName, *binding)
	return nil
}

func (evObj *EventObject) handleOutputRemoved(sched *MIMOScheduler) error {
	bindingName := evObj.Payload.(string)
	sched.outputBindings.Delete(bindingName)
	return nil
}

func (mimoSched *MIMOScheduler) handleEvent(evObj *EventObject) error {
	switch evObj.Type {
	case EVSourceAdded:
		return evObj.handleNewInputAdded(mimoSched)
	case EVSourceDrained:
		return evObj.handleSourceDrained(mimoSched)
	case EVNewPktAvailable:
		return evObj.handleNewPktAvailable(mimoSched)
	case EVNewOutputAdded:
		return evObj.handleNewOutputAdded(mimoSched)
	case EVOutputRemoved:
		return evObj.handleOutputRemoved(mimoSched)
	default:
		panic(fmt.Sprintf("unknown event type: %s", evObj.Type))
	}
}

// Run executes the scheduler, setting up the entire pipeline.
func (mimoSched *MIMOScheduler) Run(ctx context.Context) {

	// Runs a muxer service goroutine in the background
	go func() {
		defer close(mimoSched.muxerServiceChan)

		for {
			requestCh := make(chan EventObject)
			select {
			case <-ctx.Done():
				return
			case mimoSched.muxerServiceChan <- requestCh:
				evObj := <-requestCh
				err := mimoSched.handleEvent(&evObj)
				if err != nil {
					fmt.Printf("muxer service: error handling event: %v\n", err)
				}
				evObj.Result <- &EventResult{
					Error: err,
				}
			}
		}
	}()

	throttle := NewTokenBasedThrottle(mimoSched.ThrottleConfig)
	throttledChan := throttle.Run(mimoSched.muxedDataChan)
	smoothedChan := mimoSched.SmootherConfig.Run(throttledChan)

	go func() {

		for anywrappedItem := range smoothedChan {
			select {
			case <-ctx.Done():
				return
			default:
				wp, ok := anywrappedItem.(wrappedpacket)
				if !ok {
					panic(fmt.Sprintf("Demuxer: Unexpected item type: %T", anywrappedItem))
				}

				// Iterate over all active output bindings
				mimoSched.outputBindings.Range(func(key, value interface{}) bool {
					binding, _ := value.(LabeledPacketBinding)

					// Check if the packet's label matches the binding's pattern
					if binding.Pattern.MatchString(wp.label) {
						// It is the user's responsibility to keep the consumer side non-blocking, otherwise,
						// the scheduler will drop packets here.
						select {
						case binding.OutputChan <- wp.packet:
						default:
							log.Println("[DBG] Output channel full (client is slow). Drop the packet and continue.")
							// Output channel full (client is slow). Drop the packet and continue.
						}
					}
					// Return true to continue iterating over the bindings
					return true
				})
			}
		}
	}()

}
