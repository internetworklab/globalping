package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/btree"
)

type Node struct {
	Id            int
	Name          string
	InC           chan interface{}
	Dead          bool
	ItemsCopied   int
	ScheduledTime float64
	LifeSpan      float64
	staging       chan interface{}
}

func (node *Node) PrintWithIdx(idx int) {
	fmt.Printf("[%d] ID=%d, Name=%s, ScheduledTime=%f, LifeSpan=%f\n", idx, node.Id, node.Name, node.ScheduledTime, node.LifeSpan)
}

const defaultChannelBufferSize = 1024

func (nd *Node) RegisterDataEvent(evCh <-chan chan EVObject, ioTodoQueue *btree.BTree) {
	go func() {
		nd.staging = make(chan interface{}, defaultChannelBufferSize)
		defer close(nd.staging)
		defer log.Printf("node %s is drained", nd.Name)

		itemsLoaded := 0
		for {
			select {
			case item, ok := <-nd.InC:
				log.Printf("load into staging: %v", item)
				if !ok {
					return
				}
				nd.staging <- item
				itemsLoaded++
			default:
				if itemsLoaded > 0 {
					itemsLoaded = 0
					// todo: do not overwhelming the ev loop
					if ioTodoQueue.Get(nd) == nil {
						ioTodoQueue.ReplaceOrInsert(nd)

						evRequestCh, ok := <-evCh
						if !ok {
							// service channel was closed
							return
						}
						evObj := EVObject{
							Type:    EVNodeDataAvailable,
							Payload: nil,
							Result:  make(chan error),
						}
						evRequestCh <- evObj
						<-evObj.Result
					}
				}
			}

		}
	}()
}

func (nd *Node) schedDensity() float64 {
	return float64(nd.ScheduledTime) / float64(nd.LifeSpan)
}

func (n *Node) Less(item btree.Item) bool {
	if nodeItem, ok := item.(*Node); ok {

		delta := n.schedDensity() - nodeItem.schedDensity()
		if math.Abs(delta) < 0.01 {
			return !(n.Id < nodeItem.Id)
		}
		return delta < 0

	}
	return false
}

type EVType string

const (
	EVNodeDataAvailable EVType = "node_data_available"
	EVNodeAdded         EVType = "node_added"
)

type EVObject struct {
	Type    EVType
	Payload interface{}
	Result  chan error
}

// the returning channel doesn't emit anything meaningful, it's simply for synchronization
func (nd *Node) Run(outC chan<- interface{}) <-chan interface{} {
	runCh := make(chan interface{})
	headNode := nd
	go func() {
		defer close(runCh)
		defer log.Println("runCh closed", "node", headNode.Name)

		timeout := time.After(defaultTimeSlice)

		for {
			select {
			case <-timeout:
				log.Println("timeout", "node", headNode.Name)
				return
			case item, ok := <-headNode.staging:
				if !ok {
					headNode.Dead = true
					return
				}
				outC <- item
				headNode.ItemsCopied = headNode.ItemsCopied + 1
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
	mainQueue := btree.New(2)

	outC := make(chan interface{})
	evCh := make(chan chan EVObject)

	allNodes := make(map[int]*Node)

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
				switch evRequest.Type {
				case EVNodeAdded:
					newNode, ok := evRequest.Payload.(*Node)
					if !ok {
						panic("unexpected node type")
					}
					log.Printf("Added node %s to evCenter", newNode.Name)
					newNode.RegisterDataEvent(evCh, mainQueue)
				case EVNodeDataAvailable:
					headNodeItem := mainQueue.DeleteMin()
					if headNodeItem == nil {
						panic("head node item shouldn't be nil")
					}

					headNode, ok := headNodeItem.(*Node)
					if !ok {
						panic("unexpected node type")
					}

					nrCopiedPreRun := headNode.ItemsCopied
					<-headNode.Run(outC)
					nrCopiedPostRun := headNode.ItemsCopied
					scheduledTimeDelta := 1.0 / math.Max(1.0, float64(len(allNodes)))
					if nrCopiedPostRun == nrCopiedPreRun {
						// extra penalty for idling
						headNode.ScheduledTime = headNode.ScheduledTime + scheduledTimeDelta
					}
					headNode.ScheduledTime = headNode.ScheduledTime + scheduledTimeDelta
					headNode.LifeSpan = headNode.LifeSpan + 1.0

					if headNode.Dead {
						delete(allNodes, headNode.Id)
					}
				default:
					panic(fmt.Sprintf("unknown event type: %s", evRequest.Type))
				}

			}

		}
	}()

	add := func(name string) *Node {
		newNodeId := len(allNodes)
		node := &Node{
			Id:            newNodeId,
			Name:          name,
			ScheduledTime: 0.0,
			LifeSpan:      1.0,
			InC:           anonymousSource(ctx, name),
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

	// consumer goroutine
	go func() {
		stat := make(map[string]int)
		total := 0
		for muxedItem := range outC {
			fmt.Println("muxedItem: ", muxedItem)
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

	nodeA := add("A")
	nodeB := add("B")
	nodeC := add("C")

	addToEvCenter(nodeA)
	addToEvCenter(nodeB)
	addToEvCenter(nodeC)

	sig := <-sigs
	fmt.Println("signal received: ", sig, " exitting...")
}
