package pinger

import "context"

type TCPSYNPinger struct{
	PingRequest *SimplePingRequest
}

func (t *TCPSYNPinger) Ping(ctx context.Context) <-chan PingEvent {
	evCh := make(chan PingEvent)
	go func() {
		defer close(evCh)

		// todo
	}()
	return evCh
}
