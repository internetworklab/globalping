package throttle

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/google/btree"
)

const defaultTimeSlice time.Duration = 50 * time.Millisecond
const defaultMaximumChunkSize = 1024

type TSSchedEVType string

const (
	TSSchedEVNewSource   TSSchedEVType = "new_source"
	TSSchedEVNewDataTask TSSchedEVType = "new_data_task"
	TSSchedEVNodeDrained TSSchedEVType = "node_drained"
	TSSchedEVNewHook     TSSchedEVType = "new_hook"
)

type TSSchedEVObject struct {
	Type    TSSchedEVType
	Payload interface{}
	Result  chan error
}

type TimeSlicedEVLoopSched struct {
	evCh            chan chan TSSchedEVObject
	config          *TimeSlicedEVLoopSchedConfig
	outC            chan interface{}
	numEventsPassed *int
	nodeQueue       *btree.BTree

	// numbers of nodes added since the very beginning
	// we use this field for generating unique node id
	nodesAdded map[int]*TSSchedSourceNode

	defaultTimeSlice *time.Duration
	maximumChunkSize int

	// evtype -> hook name -> handler function
	customEVHandlers map[TSSchedEVType]map[string]func(evObj *TSSchedEVObject) error
}

type TimeSlicedEVLoopSchedConfig struct {
	DefaultTimeSlice *time.Duration
	BTreeOrder       *int
	MaximumChunkSize *int
}

func NewTimeSlicedEVLoopSched(config *TimeSlicedEVLoopSchedConfig) (*TimeSlicedEVLoopSched, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	sched := &TimeSlicedEVLoopSched{
		evCh:             make(chan chan TSSchedEVObject),
		config:           config,
		outC:             make(chan interface{}),
		numEventsPassed:  new(int),
		nodesAdded:       make(map[int]*TSSchedSourceNode),
		maximumChunkSize: defaultMaximumChunkSize,
		customEVHandlers: make(map[TSSchedEVType]map[string]func(evObj *TSSchedEVObject) error),
	}
	btreeOrder := 2
	if config.BTreeOrder != nil {
		if *config.BTreeOrder < 2 {
			return nil, fmt.Errorf("btree order must be at least 2")
		}
		btreeOrder = *config.BTreeOrder
	}
	sched.nodeQueue = btree.New(btreeOrder)
	*(sched.numEventsPassed) = 0

	tsUsed := defaultTimeSlice
	if config.DefaultTimeSlice != nil {
		if config.DefaultTimeSlice.Milliseconds() < 10 {
			return nil, fmt.Errorf("default time slice must be at least 10 milliseconds")
		}
		tsUsed = *config.DefaultTimeSlice
	}
	sched.defaultTimeSlice = &tsUsed

	if config.MaximumChunkSize != nil {
		if *config.MaximumChunkSize < 1 {
			return nil, fmt.Errorf("maximum chunk size must be at least 1")
		}
		sched.maximumChunkSize = *config.MaximumChunkSize
	}

	return sched, nil
}

type TSSchedCustomEVHandlerSubmission struct {
	EVType   TSSchedEVType
	HookName string
	Handler  func(evObj *TSSchedEVObject) error
}

func (tsSched *TimeSlicedEVLoopSched) RegisterCustomEVHandler(ctx context.Context, evType TSSchedEVType, hookName string, handler func(evObj *TSSchedEVObject) error) error {
	allowedCustomHookEVTypes := []TSSchedEVType{TSSchedEVNodeDrained}
	if !slices.Contains(allowedCustomHookEVTypes, evType) {
		return fmt.Errorf("invalid ev type for custom hook: %s, allowed types: %v", evType, allowedCustomHookEVTypes)
	}

	evSubCh, ok := <-tsSched.evCh
	if !ok {
		return fmt.Errorf("ev channel is already closed")
	}

	evObj := TSSchedEVObject{
		Type: TSSchedEVNewHook,
		Payload: TSSchedCustomEVHandlerSubmission{
			HookName: hookName,
			Handler:  handler,
			EVType:   evType,
		},
		Result: make(chan error),
	}

	evSubCh <- evObj

	select {
	case err, ok := <-evObj.Result:
		if !ok {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (tsSched *TimeSlicedEVLoopSched) GetOutput() <-chan interface{} {
	return tsSched.outC
}

func (tsSched *TimeSlicedEVLoopSched) Run(ctx context.Context) chan error {
	errorChan := make(chan error)
	numEventsPassed := tsSched.numEventsPassed

	go func() {
		defer close(errorChan)
		log.Println("Event loop is started")
		defer log.Println("Event loop is stopped")

		for {
			evRequestCh := make(chan TSSchedEVObject)
			select {
			case <-ctx.Done():
				log.Println("exitting event loop")
				return
			case tsSched.evCh <- evRequestCh:
				evRequest := <-evRequestCh
				*numEventsPassed = *numEventsPassed + 1

				log.Printf("Event %s, Generation: %d", evRequest.Type, *numEventsPassed)

				switch evRequest.Type {
				case TSSchedEVNewHook:
					evPayloadHook, ok := evRequest.Payload.(TSSchedCustomEVHandlerSubmission)
					if !ok {
						panic("unexpected ev payload, it's not of a type of struct TSSchedCustomEVHandlerSubmission")
					}
					evType := evPayloadHook.EVType
					if _, ok := tsSched.customEVHandlers[evType]; !ok {
						tsSched.customEVHandlers[evType] = make(map[string]func(evObj *TSSchedEVObject) error)
					}
					tsSched.customEVHandlers[evType][evPayloadHook.HookName] = evPayloadHook.Handler

					log.Printf("custom ev handler %s for ev type %s is registered", evPayloadHook.HookName, evType)

					evRequest.Result <- nil
				case TSSchedEVNewSource:
					inputChanSubmission, ok := evRequest.Payload.(*TSSchedInputChanSubmission)
					if !ok {
						panic("unexpected ev payload, it's not of a type of <-chan interface{}")
					}

					newNodeId := len(tsSched.nodesAdded)
					newNodeObject := &TSSchedSourceNode{
						Id:              newNodeId,
						InC:             inputChanSubmission.InputChan,
						ScheduledTime:   0.0,
						queuePending:    make(chan interface{}, 1),
						NumItemsCopied:  make([]int, 3),
						CurrentDataBlob: nil,
					}
					inputChanSubmission.OpaqueId = newNodeId
					tsSched.nodesAdded[newNodeId] = newNodeObject

					newNodeObject.RegisterDataEvent(tsSched.evCh, tsSched.nodeQueue, tsSched.maximumChunkSize, *tsSched.defaultTimeSlice)
					evRequest.Result <- nil
				case TSSchedEVNewDataTask:

					evPayloadNode, ok := evRequest.Payload.(*TSSchedSourceNode)
					if !ok {
						panic("unexpected ev payload, it's not of a type of struct *Node")
					}

					if tsSched.nodeQueue.ReplaceOrInsert(evPayloadNode) != nil {
						panic("the node is already in the queue, which is unexpected")
					}

					headItem := tsSched.nodeQueue.DeleteMin()
					if headItem == nil {
						panic("head item in the queue shouldn't be nil")
					}

					nodeObject, ok := headItem.(*TSSchedSourceNode)
					if !ok {
						panic("head item in the queue is not a of type struct Node")
					}
					if nodeObject == nil {
						panic("head node in the queue is nil")
					}

					itemsCopied := nodeObject.Run(tsSched.outC, nodeObject, *tsSched.defaultTimeSlice)
					nodeObject.ScheduledTime += 1.0
					nodeObject.NumItemsCopied[0] = nodeObject.NumItemsCopied[1]
					nodeObject.NumItemsCopied[1] = nodeObject.NumItemsCopied[2]
					nodeObject.NumItemsCopied[2] = itemsCopied

					evRequest.Result <- nil
				case TSSchedEVNodeDrained:
					deadNodeId, ok := evRequest.Payload.(int)
					if !ok {
						panic("unexpected ev payload, it's not of a type of int")
					}
					delete(tsSched.nodesAdded, deadNodeId)

					if handlers, ok := tsSched.customEVHandlers[TSSchedEVNodeDrained]; ok {
						for hookName, handler := range handlers {
							log.Printf("executing custom ev handler %s for ev type %s", hookName, TSSchedEVNodeDrained)
							handler(&evRequest)
						}
					}
				default:
					panic(fmt.Sprintf("unknown event type: %s", evRequest.Type))
				}
			}
		}
	}()
	return errorChan
}

type TSSchedInputChanSubmission struct {
	InputChan <-chan interface{}
	OpaqueId  int
}

// returns: (opaque-source-id interface{}, err error)
func (tsSched *TimeSlicedEVLoopSched) AddInput(ctx context.Context, inputChan <-chan interface{}) (interface{}, error) {
	evSubCh, ok := <-tsSched.evCh
	if !ok {
		return nil, fmt.Errorf("ev channel is already closed")
	}
	defer close(evSubCh)

	submission := &TSSchedInputChanSubmission{
		InputChan: inputChan,
	}

	evObj := TSSchedEVObject{
		Type:    TSSchedEVNewSource,
		Payload: submission,
		Result:  make(chan error),
	}
	evSubCh <- evObj
	select {
	case err, ok := <-evObj.Result:
		if !ok || err == nil {
			return submission.OpaqueId, nil
		}
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type TSSchedSourceNode struct {
	Id              int
	InC             <-chan interface{}
	ScheduledTime   float64
	queuePending    chan interface{}
	NumItemsCopied  []int
	CurrentDataBlob *TSSchedDataBlob
}

type TSSchedDataBlob struct {
	Chunk    []interface{}
	Capacity int
	Length   int
}

func (blob *TSSchedDataBlob) CopyTo(dst chan<- interface{}, maximumTimeSlice time.Duration) (numItemsCopied int) {
	numItemsCopied = 0
	timeout := time.After(maximumTimeSlice)
	for numItemsCopied < blob.Length {
		select {
		case <-timeout:
			return numItemsCopied
		case dst <- blob.Chunk[blob.Capacity-blob.Length+numItemsCopied]:
			numItemsCopied++
		default:
			return numItemsCopied
		}
	}
	return numItemsCopied
}

func streamToChunkedStream(source <-chan interface{}, maximumTimeSlice time.Duration, maxChunkSize int) <-chan TSSchedDataBlob {
	if maxChunkSize < 1 {
		panic("max chunk size must be at least 1")
	}

	outC := make(chan TSSchedDataBlob)
	timeout := time.After(maximumTimeSlice)
	go func() {
		defer close(outC)

		doFlush := func() TSSchedDataBlob {
			return TSSchedDataBlob{
				Chunk:    make([]interface{}, maxChunkSize),
				Capacity: maxChunkSize,
				Length:   0,
			}
		}

		blob := doFlush()

		for {
			select {
			case <-timeout:
				outC <- blob
				blob = doFlush()
			case item, ok := <-source:
				if !ok {
					// source is drained
					outC <- blob
					blob = doFlush()
					return
				}
				blob.Chunk[blob.Length] = item
				blob.Length++
				if blob.Length >= blob.Capacity {
					outC <- blob
					blob = doFlush()
				}
			}
		}
	}()

	return outC
}

func (nd *TSSchedSourceNode) RegisterDataEvent(
	evCh <-chan chan TSSchedEVObject,
	nodeQueue *btree.BTree,
	maximumChunkSize int,
	maximumTimeSlice time.Duration,
) {
	go func() {
		for blob := range streamToChunkedStream(nd.InC, maximumTimeSlice, maximumChunkSize) {
			if blob.Length <= 0 {
				// skip null chunk
				continue
			}
			blobPtr := &blob
			for blobPtr.Length > 0 {
				evSubCh := <-evCh
				nd.queuePending <- struct{}{}
				nd.CurrentDataBlob = blobPtr
				evObj := TSSchedEVObject{
					Type:    TSSchedEVNewDataTask,
					Payload: nd,
					Result:  make(chan error),
				}

				evSubCh <- evObj
				<-evObj.Result

				<-nd.queuePending
			}
		}

		go func() {
			evSubCh, ok := <-evCh
			if !ok {
				return
			}
			evObj := TSSchedEVObject{
				Type:    TSSchedEVNodeDrained,
				Payload: nd.Id,
				Result:  make(chan error),
			}
			evSubCh <- evObj
			<-evObj.Result
		}()
	}()
}

func (nd *TSSchedSourceNode) TotalCopied() int {
	sum := 0
	for _, v := range nd.NumItemsCopied {
		sum += v
	}
	return sum
}

func (n *TSSchedSourceNode) Less(item btree.Item) bool {
	if n == nil || item == nil {
		panic("comparing node against nil")
	}

	if nodeItem, ok := item.(*TSSchedSourceNode); ok {
		if n.TotalCopied() == nodeItem.TotalCopied() {
			return !(n.Id < nodeItem.Id)
		}
		return n.TotalCopied() < nodeItem.TotalCopied()
	}

	panic(fmt.Sprintf("comparing node with unexpected item type: %T", item))
}

// the returning channel doesn't emit anything meaningful, it's simply for synchronization
func (nd *TSSchedSourceNode) Run(outC chan<- interface{}, nodeObject *TSSchedSourceNode, duration time.Duration) (itemsCopied int) {
	itemsCopied = nodeObject.CurrentDataBlob.CopyTo(outC, duration)
	nodeObject.CurrentDataBlob.Length -= itemsCopied
	return itemsCopied
}
