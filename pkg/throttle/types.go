package throttle

import (
	"context"
)

// A generic MISO scheduler doesn't care about the label at all,
// it purely focus on scheduling packets from multiple inputs to a single output.
// However, since the type for packets we used here is interface{},
// the user is free to design its own struct for wrapping packets to make them tagged.
// For example, a packet of interface{} could be a MySimpleWrappedPacket{OriginPacket: xxx, Label: yyy}
type GenericMISOScheduler interface {
	// returns: (opaque-source-id interface{}, err error)
	AddInput(ctx context.Context, inputChan <-chan interface{}) (interface{}, error)
	GetOutput() <-chan interface{}
}

type MISOMuxer interface {
	AddInput(inputChan <-chan interface{}, label string) error
	GetOutput() <-chan interface{}
}

type SIMODemuxer interface {
	AddOutput(outputChan chan<- interface{}, labelPattern string, labelBindingName string) error
	RemoveOutput(labelBindingName string) error
	GetInput() chan<- interface{}
}

type SISOPipe interface {
	GetInput() chan<- interface{}
	GetOutput() <-chan interface{}
}

type EventResult struct {
	Error error
}

// Wrappedpacket encapsulates the original packet with its source label.
type Wrappedpacket struct {
	label  string
	packet interface{}
}
