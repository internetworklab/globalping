package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/btree"
)

type DataBlob struct {
	BufferedChan chan interface{}
	Size         int
	Remaining    int
}

type Node struct {
	Id             int
	Name           string
	InC            chan interface{}
	ScheduledTime  float64
	queuePending   chan interface{}
	NumItemsCopied []int
}

const defaultChannelBufferSize = 1024

func (nd *Node) RegisterDataEvent(evCh <-chan chan EVObject, nodeQueue *btree.BTree) {
	toBeSendFragments := make(chan *DataBlob)
	taskSeq := 0
	go func() {
		defer close(toBeSendFragments)
		for dataBlob := range toBeSendFragments {
			for dataBlob.Remaining > 0 {
				evSubCh := <-evCh
				nd.queuePending <- struct{}{}
				nodeQueue.ReplaceOrInsert(nd)
				evObj := EVObject{
					Type:    EVNewDataTask,
					Payload: dataBlob,
					Result:  make(chan error),
				}

				evSubCh <- evObj
				<-evObj.Result
				taskSeq++
			}
		}
	}()

	go func() {
		defer log.Printf("node %s is drained", nd.Name)

		staging := make(chan interface{}, defaultChannelBufferSize)
		itemsLoaded := 0
		for item := range nd.InC {
			select {
			case staging <- item:
				itemsLoaded++
			default:
				toBeSendFragments <- &DataBlob{
					BufferedChan: staging,
					Size:         itemsLoaded,
					Remaining:    itemsLoaded,
				}
				itemsLoaded = 0
				staging = make(chan interface{}, defaultChannelBufferSize)
			}
		}

		if itemsLoaded > 0 {
			// flush the remaining items in buffer
			toBeSendFragments <- &DataBlob{
				BufferedChan: staging,
				Size:         itemsLoaded,
				Remaining:    itemsLoaded,
			}
			itemsLoaded = 0
		}
	}()
}

func (nd *Node) TotalCopied() int {
	sum := 0
	for _, v := range nd.NumItemsCopied {
		sum += v
	}
	return sum
}

func (n *Node) Less(item btree.Item) bool {
	if n == nil || item == nil {
		return true
	}

	if nodeItem, ok := item.(*Node); ok {
		if n.TotalCopied() == nodeItem.TotalCopied() {
			return !(n.Id < nodeItem.Id)
		}
		return n.TotalCopied() < nodeItem.TotalCopied()
	}

	panic(fmt.Sprintf("comparing node with unexpected item type: %T", item))
}

type EVType string

const (
	EVNodeAdded   EVType = "node_added"
	EVNewDataTask EVType = "new_data_task"
)

type EVObject struct {
	Type    EVType
	Payload interface{}
	Result  chan error
}

// the returning channel doesn't emit anything meaningful, it's simply for synchronization
func (nd *Node) Run(outC chan<- interface{}, nodeObject *Node, dataBlob *DataBlob) <-chan int {
	runCh := make(chan int)

	var itemsCopied *int = new(int)
	*itemsCopied = 0

	go func() {
		defer func() {
			n := *itemsCopied
			runCh <- n
		}()

		timeout := time.After(defaultTimeSlice)

		for {
			select {
			case <-timeout:
				// todo: Some data may still in the buffer when timeout.
				return
			case item, ok := <-dataBlob.BufferedChan:
				if !ok {
					return
				}
				outC <- item
				*itemsCopied = *itemsCopied + 1
				dataBlob.Remaining--
			default:
				return
			}
		}
	}()
	return runCh
}

func anonymousSource(ctx context.Context, content string, limit *int) chan interface{} {
	outC := make(chan interface{})
	numItemsCopied := 0
	go func() {
		defer close(outC)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				outC <- content
				numItemsCopied++
				if limit != nil && *limit > 0 && numItemsCopied >= *limit {
					log.Printf("source %s reached limit %d, stopping", content, *limit)
					return
				}
			}
		}
	}()
	return outC
}

const defaultTimeSlice time.Duration = 50 * time.Millisecond

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodeQueue := btree.New(2)

	outC := make(chan interface{})
	evCh := make(chan chan EVObject)

	var nodesCount *int = new(int)
	*nodesCount = 0

	nodesCountMutex := new(sync.Mutex)

	var numEventsPassed *int = new(int)
	*numEventsPassed = 0

	add := func(name string, limit *int) *Node {
		nodesCountMutex.Lock()
		defer nodesCountMutex.Unlock()

		node := &Node{
			Id:   *nodesCount,
			Name: name,
			InC:  anonymousSource(ctx, name, limit),

			// the buffer size of this channel `queuePending` must be 1,
			// to ensure that, for every moment, at most 1 node instance is in-queue
			queuePending:   make(chan interface{}, 1),
			NumItemsCopied: make([]int, 3),
		}
		node.NumItemsCopied[0] = 0
		node.NumItemsCopied[1] = 0
		node.NumItemsCopied[2] = 0

		*nodesCount = *nodesCount + 1

		return node
	}

	addToEvCenter := func(node *Node) {
		evSubCh, ok := <-evCh
		if !ok {
			panic("evCh is closed")
		}
		evObj := EVObject{
			Type:    EVNodeAdded,
			Payload: node,
			Result:  make(chan error),
		}
		evSubCh <- evObj
		<-evObj.Result
	}

	aLim := 8000000
	bLim := 16000000
	cLim := 24000000
	dLim := 32000000
	eLim := 40000000
	fLim := 48000000

	go func() {
		log.Println("evCenter started")

		defer close(evCh)

		for {
			evRequestCh := make(chan EVObject)
			select {
			case <-ctx.Done():
				return
			case evCh <- evRequestCh:
				evRequest := <-evRequestCh
				*numEventsPassed = *numEventsPassed + 1

				log.Printf("Event %s, Generation: %d", evRequest.Type, *numEventsPassed)

				if *numEventsPassed == 8000 {
					go func() {
						newNode := add("D", &dLim)
						log.Printf("reached %d generations, adding new node %s", *numEventsPassed, newNode.Name)
						addToEvCenter(newNode)
					}()

				}

				if *numEventsPassed == 16000 {
					go func() {
						newNode := add("E", &eLim)
						log.Printf("reached %d generations, adding new node %s", *numEventsPassed, newNode.Name)
						addToEvCenter(newNode)
					}()
				}

				if *numEventsPassed == 24000 {
					go func() {
						newNode := add("F", &fLim)
						log.Printf("reached %d generations, adding new node %s", *numEventsPassed, newNode.Name)
						addToEvCenter(newNode)
					}()
				}

				switch evRequest.Type {
				case EVNodeAdded:
					newNode, ok := evRequest.Payload.(*Node)
					if !ok {
						panic("unexpected node type")
					}
					log.Printf("Added node %s to evCenter", newNode.Name)
					newNode.RegisterDataEvent(evCh, nodeQueue)
					evRequest.Result <- nil
				case EVNewDataTask:
					headItem := nodeQueue.DeleteMin()
					if headItem == nil {
						panic("head item in the queue shouldn't be nil")
					}

					nodeObject, ok := headItem.(*Node)
					if !ok {
						panic("head item in the queue is not a of type struct Node")
					}

					dataBlob, ok := evRequest.Payload.(*DataBlob)
					if !ok {
						panic("payload of event is not a of type struct DataBlob")
					}

					itemsCopied := <-nodeObject.Run(outC, nodeObject, dataBlob)
					nodeObject.ScheduledTime += 1.0
					nodeObject.NumItemsCopied[0] = nodeObject.NumItemsCopied[1]
					nodeObject.NumItemsCopied[1] = nodeObject.NumItemsCopied[2]
					nodeObject.NumItemsCopied[2] = itemsCopied
					evRequest.Result <- nil

					// release the queue lock, to enable the node be re-queued again
					<-nodeObject.queuePending

				default:
					panic(fmt.Sprintf("unknown event type: %s", evRequest.Type))
				}

			}

		}
	}()

	// consumer goroutine
	go func() {
		stat := make(map[string]int)
		total := 0
		for muxedItem := range outC {
			stat[muxedItem.(string)]++
			total++
			if total%1000 == 0 {
				for k, v := range stat {
					fmt.Printf("%s: %d, %.2f%%\n", k, v, 100*float64(v)/float64(total))
				}
			}
		}
	}()

	nodeA := add("A", &aLim)
	nodeB := add("B", &bLim)
	nodeC := add("C", &cLim)

	addToEvCenter(nodeA)
	addToEvCenter(nodeB)
	addToEvCenter(nodeC)

	sig := <-sigs
	fmt.Println("signal received: ", sig, " exitting...")
}
