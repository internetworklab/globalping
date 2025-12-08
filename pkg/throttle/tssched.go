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
				log.Println("inside event loop, context is done or cancelled")
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

					newNodeObject.RegisterDataEvent(tsSched.evCh, tsSched.nodeQueue, tsSched.maximumChunkSize)
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
					if nodeObject.Dead {
						log.Printf("node %d is dead, skipping", nodeObject.Id)
						continue
					}

					itemsCopied, nodeIsDead := nodeObject.Run(tsSched.outC, nodeObject, *tsSched.defaultTimeSlice)
					nodeObject.ScheduledTime += 1.0
					nodeObject.NumItemsCopied[0] = nodeObject.NumItemsCopied[1]
					nodeObject.NumItemsCopied[1] = nodeObject.NumItemsCopied[2]
					nodeObject.NumItemsCopied[2] = itemsCopied

					if nodeIsDead {
						nodeObject.Dead = true
						delete(tsSched.nodesAdded, nodeObject.Id)

						go func() {
							evSubCh, ok := <-tsSched.evCh
							if !ok {
								return
							}
							evObj := TSSchedEVObject{
								Type:    TSSchedEVNodeDrained,
								Payload: nodeObject.Id,
								Result:  make(chan error),
							}
							evSubCh <- evObj
							<-evObj.Result
						}()
					}

					evRequest.Result <- nil
				case TSSchedEVNodeDrained:
					if handlers, ok := tsSched.customEVHandlers[TSSchedEVNodeDrained]; ok {
						for _, handler := range handlers {
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
	Dead            bool
	InC             <-chan interface{}
	ScheduledTime   float64
	queuePending    chan interface{}
	NumItemsCopied  []int
	CurrentDataBlob *TSSchedDataBlob
}

type TSSchedDataBlob struct {
	BufferedChan chan interface{}
	Size         int
	Remaining    int
}

func (nd *TSSchedSourceNode) RegisterDataEvent(
	evCh <-chan chan TSSchedEVObject,
	nodeQueue *btree.BTree,
	maximumChunkSize int,
) {
	toBeSendFragments := make(chan *TSSchedDataBlob)

	go func() {
		for dataBlob := range toBeSendFragments {
			for dataBlob.Remaining > 0 {
				evSubCh := <-evCh
				nd.queuePending <- struct{}{}
				nd.CurrentDataBlob = dataBlob
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
	}()

	go func() {
		itemsLoaded := 0
		totalItemsProcessed := 0

		staging := make(chan interface{}, maximumChunkSize)
		temporaryBuf := make([]interface{}, 0)
		for item := range nd.InC {
			totalItemsProcessed++

			if len(temporaryBuf) > 0 {
				for _, item := range temporaryBuf {
					staging <- item
					itemsLoaded++
				}
				temporaryBuf = make([]interface{}, 0)
			}
			select {
			case staging <- item:
				itemsLoaded++
			default:
				temporaryBuf = append(temporaryBuf, item)
				toBeSendFragments <- &TSSchedDataBlob{
					BufferedChan: staging,
					Size:         itemsLoaded,
					Remaining:    itemsLoaded,
				}
				itemsLoaded = 0
				staging = make(chan interface{}, maximumChunkSize)
			}
		}

		if itemsLoaded > 0 {
			// flush the remaining items in buffer
			toBeSendFragments <- &TSSchedDataBlob{
				BufferedChan: staging,
				Size:         itemsLoaded,
				Remaining:    itemsLoaded,
			}
			itemsLoaded = 0
		}
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
func (nd *TSSchedSourceNode) Run(outC chan<- interface{}, nodeObject *TSSchedSourceNode, duration time.Duration) (itemsCopied int, nodeIsDead bool) {

	itemsCopied = 0

	timeout := time.After(duration)

	for {
		select {
		case <-timeout:
			// todo: Some data may still in the buffer when timeout.
			return itemsCopied, false
		case item, ok := <-nodeObject.CurrentDataBlob.BufferedChan:
			if !ok {
				return itemsCopied, true
			}
			outC <- item
			itemsCopied = itemsCopied + 1
			nodeObject.CurrentDataBlob.Remaining--
		default:
			return itemsCopied, false
		}
	}
}
