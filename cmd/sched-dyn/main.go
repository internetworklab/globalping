package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/btree"
)

type DataBlob struct {
	BufferedChan chan interface{}
	Size int
}

type Node struct {
	Id                int
	Name              string
	InC               chan interface{}
	ItemsCopied       int
	ScheduledTime     float64
	CurrentGeneration *int
	BirthGeneration   int
	Mux               sync.Mutex
	queuePending      chan interface{}
}

func (nd *Node) GetAge() float64 {
	return float64(*nd.CurrentGeneration - nd.BirthGeneration)
}

func (node *Node) PrintWithIdx(idx int) {
	fmt.Printf("[%d] ID=%d, Name=%s, ScheduledTime=%f, Age=%f\n", idx, node.Id, node.Name, node.ScheduledTime, node.GetAge())
}

const defaultChannelBufferSize = 1024

func (nd *Node) RegisterDataEvent(evCh <-chan chan EVObject, nodeQueue *btree.BTree) {
	go func() {
		defer log.Printf("node %s is drained", nd.Name)

		toBeSendFragments := make(chan DataBlob)
		taskSeq := 0
		go func() {
			defer close(toBeSendFragments)
			for dataBlob := range toBeSendFragments {
				nd.queuePending <- struct{}{}
				evObj := EVObject{
					Type:    EVNewDataTask,
					Payload: dataBlob,
					Result:  make(chan error),
				}
				evSubCh := <-evCh
				evSubCh <- evObj
				<-evObj.Result
				taskSeq++
			}
		}()

		staging := make(chan interface{}, defaultChannelBufferSize)
		itemsLoaded := 0
		for item := range nd.InC {
			select {
			case staging <- item:
				itemsLoaded++
			default:
				toBeSendFragments <- DataBlob{
					BufferedChan: staging,
					Size: itemsLoaded,
				}
				itemsLoaded = 0
				staging = make(chan interface{}, defaultChannelBufferSize)
			}
		}

		if itemsLoaded > 0 {
			// flush the remaining items in buffer
			itemsLoaded = 0
			toBeSendFragments <- DataBlob{
				BufferedChan: staging,
				Size: itemsLoaded,
			}
		}
	}()
}

func (nd *Node) schedDensity() float64 {
	return float64(nd.ScheduledTime) / math.Max(1.0, nd.GetAge())
}

func (n *Node) Less(item btree.Item) bool {
	if nodeItem, ok := item.(*Node); ok {

		delta := n.schedDensity() - nodeItem.schedDensity()
		if math.Abs(delta) < 0.01 {
			return !(n.Id < nodeItem.Id)
		}
		return delta < 0

	}
	panic("comparing node with unexpected item type")
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
func (nd *Node) Run(outC chan<- interface{}, nodeObject *Node, dataBlob DataBlob) <-chan interface{} {
	runCh := make(chan interface{})

	go func() {
		defer close(runCh)

		timeout := time.After(defaultTimeSlice)

		for {
			select {
			case <-timeout:
				return
			case item, ok := <-dataBlob.BufferedChan:
				if !ok {
					return
				}
				outC <- item
				nodeObject.ItemsCopied++
			default:
				return
			}
		}
	}()
	return runCh
}

func anonymousSource(ctx context.Context, content string) chan interface{} {
	outC := make(chan interface{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				outC <- content
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

	allNodes := make(map[int]*Node)

	var numEventsPassed *int = new(int)
	*numEventsPassed = 0

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
				*numEventsPassed++

				log.Printf("Event %s, Generation: %d", evRequest.Type, *numEventsPassed)

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

					dataBlob, ok := evRequest.Payload.(DataBlob)
					if !ok {
						panic("payload of event is not a of type struct DataBlob")
					}

					go func() {
						log.Println("running node", nodeObject.Name)
						defer log.Println("node", nodeObject.Name, "finished")

						nodeObject.Mux.Lock()
						defer nodeObject.Mux.Unlock()

						nrCopiedPreRun := nodeObject.ItemsCopied
						<-nodeObject.Run(outC, nodeObject, dataBlob)
						nrCopiedPostRun := nodeObject.ItemsCopied
						if nrCopiedPostRun == nrCopiedPreRun {
							// extra penalty for idling
							nodeObject.ScheduledTime += 1.0
						}
						nodeObject.ScheduledTime += 1.0

						// release the queue lock, to enable the node be re-queued again
						<-nodeObject.queuePending
					}()
					evRequest.Result <- nil
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
			// fmt.Println("muxedItem: ", muxedItem)
			stat[muxedItem.(string)]++
			total++
			if total%1000 == 0 {
				for k, v := range stat {
					fmt.Printf("%s: %d, %.2f%%\n", k, v, 100*float64(v)/float64(total))
				}
				stat = make(map[string]int)
			}
		}
	}()

	add := func(name string) *Node {
		newNodeId := len(allNodes)
		node := &Node{
			Id:                newNodeId,
			Name:              name,
			ScheduledTime:     0.0,
			CurrentGeneration: numEventsPassed,
			BirthGeneration:   *numEventsPassed,
			InC:               anonymousSource(ctx, name),
			Mux:               sync.Mutex{},

			// the buffer size of this channel `queuePending` must be 1,
			// to ensure that, for every moment, at most 1 node instance is in-queue
			queuePending: make(chan interface{}, 1),
		}
		allNodes[newNodeId] = node
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

	nodeA := add("A")
	nodeB := add("B")
	nodeC := add("C")

	addToEvCenter(nodeA)
	addToEvCenter(nodeB)
	addToEvCenter(nodeC)

	sig := <-sigs
	fmt.Println("signal received: ", sig, " exitting...")
}
