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
	DataSource    <-chan interface{}
	Dead          bool
	ItemsCopied   int
	ScheduledTime float64
	LifeSpan      float64
}

func (node *Node) PrintWithIdx(idx int) {
	fmt.Printf("[%d] ID=%d, Name=%s, ScheduledTime=%f, LifeSpan=%f\n", idx, node.Id, node.Name, node.ScheduledTime, node.LifeSpan)
}

const (
	CMD_ADD   = "add"
	CMD_RUN   = "run"
	CMD_SKIP  = "skip"
	CMD_DEL   = "del"
	CMD_EXIT  = "exit"
	CMD_PRINT = "print"
)

const defaultChannelBufferSize = 1024

func (nd *Node) RegisterDataEvent(evCh <-chan chan EVObject) {
	dataSource := nd.DataSource
	go func() {
		serviceChannelClosed := false
		defer func() {
			if !serviceChannelClosed {
				go func() {
					evRequestCh, ok := <-evCh
					if !ok {
						// service channel was really closed
						return
					}

					evObj := EVObject{
						Type:    EVNodeDrained,
						Payload: nd,
						Result:  make(chan error),
					}
					evRequestCh <- evObj
					<-evObj.Result
				}()
			}
		}()

		for range dataSource {
			evRequestCh, ok := <-evCh
			if !ok {
				// service channel was closed
				serviceChannelClosed = true
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
	EVNodeDrained       EVType = "node_drained"
	EVNodeDataAvailable EVType = "node_data_available"
	EVNodeAdded         EVType = "node_added"
	EVPrintNodes        EVType = "print_nodes"
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
		timeout := time.After(defaultTimeSlice)

		for {
			select {
			case <-timeout:
				return
			case item, ok := <-headNode.InC:
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

func anonymousSource(ctx context.Context, content string) <-chan interface{} {
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
	alternateQueue := btree.New(2)

	outC := make(chan interface{})
	evCh := make(chan chan EVObject)

	print := func(queue *btree.BTree, name string) {
		fmt.Println("BEGIN------ ", name, " -----------")
		var rank *int = new(int)
		*rank = 0
		iteratorFn := func(item btree.Item) bool {
			node := item.(*Node)
			node.PrintWithIdx(*rank)
			*rank = *rank + 1
			return true
		}
		queue.Ascend(iteratorFn)
		fmt.Println("END--------- ", name, " -------------")
	}

	go func() {
		defer close(evCh)

		for {
			evRequestCh := make(chan EVObject)
			select {
			case <-ctx.Done():
				return
			case evCh <- evRequestCh:
				evRequest := <-evRequestCh
				switch evRequest.Type {
				case EVPrintNodes:
					print(mainQueue, "main")
					print(alternateQueue, "alternate")
				case EVNodeAdded:
					newNode, ok := evRequest.Payload.(*Node)
					if !ok {
						panic("unexpected node type")
					}
					// new nodes are always added to the alternate queue
					alternateQueue.ReplaceOrInsert(newNode)
					newNode.RegisterDataEvent(evCh)
				case EVNodeDrained:
					nodeItem, ok := evRequest.Payload.(*Node)
					if !ok {
						panic("unexpected node type")
					}
					if mainQueue.Delete(nodeItem) == nil {
						panic("removing node does not exist")
					}
				case EVNodeDataAvailable:

					headNodeItem := mainQueue.DeleteMin()
					if headNodeItem == nil {
						if alternateQueue.Len() == 0 {
							fmt.Println("no node to run")
							continue
						}
						mainQueue, alternateQueue = alternateQueue, mainQueue
						headNodeItem = mainQueue.DeleteMin()
					}

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
					scheduledTimeDelta := 1.0 / float64(mainQueue.Len()+alternateQueue.Len())
					if nrCopiedPostRun == nrCopiedPreRun {
						// extra penalty for idling
						headNode.ScheduledTime = headNode.ScheduledTime + scheduledTimeDelta
					}
					headNode.ScheduledTime = headNode.ScheduledTime + scheduledTimeDelta
					headNode.LifeSpan = headNode.LifeSpan + 1.0

					alternateQueue.ReplaceOrInsert(headNode)
				default:
					panic(fmt.Sprintf("unknown event type: %s", evRequest.Type))
				}

			}

		}
	}()

	add := func() *Node {
		newNodeId := mainQueue.Len() + alternateQueue.Len()
		return &Node{
			Id:            newNodeId,
			ScheduledTime: 0.0,
			LifeSpan:      1.0,
			InC:           make(chan interface{}, defaultChannelBufferSize),
		}
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

	sourceA := anonymousSource(ctx, "A")
	sourceB := anonymousSource(ctx, "B")
	sourceC := anonymousSource(ctx, "C")

	nodeA := add()
	nodeA.DataSource = sourceA
	nodeB := add()
	nodeB.DataSource = sourceB
	nodeC := add()
	nodeC.DataSource = sourceC

	addToEvCenter(nodeA)
	log.Println("nodeA added to evCenter")
	addToEvCenter(nodeB)
	log.Println("nodeB added to evCenter")
	addToEvCenter(nodeC)
	log.Println("nodeC added to evCenter")

	sig := <-sigs
	fmt.Println("signal received: ", sig, " exitting...")
}
