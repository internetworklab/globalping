package throttle

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/btree"
)

const defaultTimeSlice time.Duration = 50 * time.Millisecond
const defaultMaximumChunkSize = 1024

type TSSchedEVType string

const (
	TSSchedEVNewSource    TSSchedEVType = "new_source"
	TSSchedEVNewDataTask  TSSchedEVType = "new_data_task"
	TSSchedEVNodeDrained  TSSchedEVType = "node_drained"
	TSSchedEVRemoveSource TSSchedEVType = "remove_source"
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

	nodeIdGenerator int

	defaultTimeSlice *time.Duration
	maximumChunkSize int
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

func (tsSched *TimeSlicedEVLoopSched) RemoveInput(ctx context.Context, opaqueSourceId interface{}) error {
	evSubCh, ok := <-tsSched.evCh
	if !ok {
		return fmt.Errorf("ev channel is already closed")
	}

	evObj := TSSchedEVObject{
		Type:    TSSchedEVRemoveSource,
		Payload: opaqueSourceId,
		Result:  make(chan error),
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
				log.Printf("[DBG] tssched received event: %+v", evRequest)

				switch evRequest.Type {
				case TSSchedEVNewSource:
					inputChanSubmission, ok := evRequest.Payload.(*TSSchedInputChanSubmission)
					if !ok {
						panic("unexpected ev payload, it's not of a type of <-chan interface{}")
					}

					newNodeId := tsSched.nodeIdGenerator
					tsSched.nodeIdGenerator++
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

					log.Printf("[DBG] tssched received new data task for node with id %d", evPayloadNode.Id)

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

					if _, ok := tsSched.nodesAdded[nodeObject.Id]; !ok {
						// node is already removed somehow, just throw away all its unread content
						log.Printf("[DBG] draining node with id %d, but it's already removed", nodeObject.Id)
						nodeObject.CurrentDataBlob.NextRead = nodeObject.CurrentDataBlob.Length
						evRequest.Result <- nil
						continue
					}

					itemsCopied := nodeObject.Run(tsSched.outC, nodeObject, *tsSched.defaultTimeSlice)
					nodeObject.ScheduledTime += 1.0
					if itemsCopied > 0 {
						nodeObject.NumItemsCopied[0] = nodeObject.NumItemsCopied[1]
						nodeObject.NumItemsCopied[1] = nodeObject.NumItemsCopied[2]
						nodeObject.NumItemsCopied[2] = itemsCopied
					}

					log.Printf("[DBG] tssched copied %d items to output channel", itemsCopied)

					evRequest.Result <- nil
				case TSSchedEVNodeDrained, TSSchedEVRemoveSource:
					deadNodeId, ok := evRequest.Payload.(int)
					if !ok {
						panic("unexpected ev payload, it's not of a type of int")
					}
					log.Printf("[DBG] tssched removing node with id %d", deadNodeId)
					delete(tsSched.nodesAdded, deadNodeId)

					evRequest.Result <- nil
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
	NextRead int
	Length   int
}

func (blob *TSSchedDataBlob) CopyTo(dst chan<- interface{}, maximumTimeSlice time.Duration) (numItemsCopied int) {
	numItemsCopied = 0
	timeout := time.After(maximumTimeSlice)
	for numItemsCopied < blob.Length-blob.NextRead {
		select {
		case <-timeout:
			log.Printf("[DBG] timeout Maximum time slice is %s", maximumTimeSlice)
			return numItemsCopied
		case dst <- blob.Chunk[blob.NextRead+numItemsCopied]:
			numItemsCopied++
		}
	}
	return numItemsCopied
}

func streamToChunkedStream(source <-chan interface{}, maximumTimeSlice time.Duration, maxChunkSize int) <-chan TSSchedDataBlob {
	if maxChunkSize < 1 {
		panic("max chunk size must be at least 1")
	}

	outC := make(chan TSSchedDataBlob)
	go func() {
		defer close(outC)

		doFlush := func() TSSchedDataBlob {
			return TSSchedDataBlob{
				Chunk:    make([]interface{}, maxChunkSize),
				NextRead: 0,
				Length:   0,
			}
		}

		blob := doFlush()
		ticker := time.NewTicker(maximumTimeSlice)

		for {
			select {
			case <-ticker.C:
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
				if blob.Length >= cap(blob.Chunk) {
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
			for blobPtr.NextRead < blobPtr.Length {
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
	nodeObject.CurrentDataBlob.NextRead += itemsCopied
	return itemsCopied
}
