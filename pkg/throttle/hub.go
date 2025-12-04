package throttle

// It's a hub, shared by multiple users, each user access the hub through a proxy,
// the hub throttles the traffic of user's write and read, and * all users are subjected
// to the same quota zone * .
// that is to say: max_speed( all users ) <= defined_shared_quota

import (
	"context"
	"fmt"
	"math/rand"
)

type ServiceRequest struct {
	Func   func(ctx context.Context) error
	Result chan error
}

type SharedThrottleProxy interface {
	GetReader() <-chan interface{}
	GetWriter() chan<- interface{}
	Close()
}

type SharedThrottleProxyEntry struct {
	// In: From Proxy user to the Hub, i.e. the proxy user writes, while the hub reads.
	In chan interface{}

	// Out: From the Hub to the Proxy user, i.e. the hub writes, while the proxy user reads.
	Out chan interface{}

	smoothedInChan chan interface{}
	labelKey       string
}

type SharedThrottleProxyImpl struct {
	id       int
	hubEntry *SharedThrottleProxyEntry
}

func (proxy *SharedThrottleProxyImpl) GetReader() <-chan interface{} {
	return proxy.hubEntry.Out
}

func (proxy *SharedThrottleProxyImpl) GetWriter() chan<- interface{} {
	return proxy.hubEntry.In
}

func (proxy *SharedThrottleProxyImpl) Close() {
	close(proxy.hubEntry.In)
}

type SharedThrottleHub struct {
	proxies     map[int]*SharedThrottleProxyEntry
	serviceChan chan chan ServiceRequest
	mimoSched   *MIMOScheduler
}

type SharedThrottleHubConfig struct {
	MIMOScheduler *MIMOScheduler
}

func NewICMPTransceiveHub(config *SharedThrottleHubConfig) *SharedThrottleHub {
	return &SharedThrottleHub{
		proxies:     make(map[int]*SharedThrottleProxyEntry),
		serviceChan: make(chan chan ServiceRequest),
		mimoSched:   config.MIMOScheduler,
	}
}

func (hub *SharedThrottleHub) Run(ctx context.Context) {

	go func() {
		defer close(hub.serviceChan)

		for {
			requestCh := make(chan ServiceRequest)
			select {
			case <-ctx.Done():
				return
			case hub.serviceChan <- requestCh:
				request := <-requestCh
				err := request.Func(ctx)
				request.Result <- err
				close(request.Result)
			}
		}
	}()
}

func (hub *SharedThrottleHub) generateNextProxyID() int {
	result := -1
	maxRetries := 10
	for {
		result = rand.Intn(0xffff) & 0xffff
		if _, ok := hub.proxies[result]; !ok {
			break
		}
		maxRetries--
		if maxRetries < 0 {
			panic("failed to generate next proxy ID")
		}
	}
	return result
}

type TestPacketType string

const (
	TestPacketTypePing TestPacketType = "ping"
	TestPacketTypePong TestPacketType = "pong"
)

type TestPacket struct {
	Host string
	Type TestPacketType
	Seq  int
	Id   int
}

func (hub *SharedThrottleHub) GetProxy() SharedThrottleProxy {
	requestCh, ok := <-hub.serviceChan
	if !ok {
		// the hub is already shutdown
		return nil
	}

	defer close(requestCh)

	var handlerId *int = new(int)
	var hubEntry *SharedThrottleProxyEntry = &SharedThrottleProxyEntry{
		In:  make(chan interface{}),
		Out: make(chan interface{}),
	}

	fn := func(ctx context.Context) error {
		newId := hub.generateNextProxyID()
		hub.proxies[newId] = hubEntry
		*handlerId = newId

		return nil
	}
	request := ServiceRequest{
		Func:   fn,
		Result: make(chan error),
	}
	requestCh <- request
	<-request.Result

	hubEntry.labelKey = fmt.Sprintf("%d", *handlerId)
	err := hub.mimoSched.AddInput(hubEntry.In, hubEntry.labelKey)
	if err != nil {
		panic(fmt.Sprintf("failed to add input to mimo scheduler: %v", err))
	}

	hubEntry.smoothedInChan = make(chan interface{})
	err = hub.mimoSched.AddOutput(hubEntry.smoothedInChan, hubEntry.labelKey, hubEntry.labelKey)
	if err != nil {
		panic(fmt.Sprintf("failed to add output to mimo scheduler: %v", err))
	}

	go func() {
		for pktIn := range hubEntry.smoothedInChan {
			if testPkt, ok := pktIn.(TestPacket); ok && testPkt.Type == TestPacketTypePing {
				pong := TestPacket{
					Type: TestPacketTypePong,
					Seq:  testPkt.Seq,
					Id:   testPkt.Id,
					Host: testPkt.Host,
				}
				hubEntry.Out <- pong
				continue
			}

			// if have no idea what to do with the packet, leave it as is
			hubEntry.Out <- pktIn
		}
	}()

	return &SharedThrottleProxyImpl{
		id:       *handlerId,
		hubEntry: hubEntry,
	}
}
