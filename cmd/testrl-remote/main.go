package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgratelimit "example.com/rbmq-demo/pkg/ratelimit"
)

type Event struct {
	Class string
	Seq   int
	Date  time.Time
}

type EventsHandler struct {
}

func (req *Event) String() string {
	return fmt.Sprintf("[%s] Seq=%d, Date=%s", req.Class, req.Seq, req.Date.Format(time.RFC3339Nano))
}

func (evHandler *EventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	cls := r.URL.Query().Get("class")
	dur, err := time.ParseDuration(r.URL.Query().Get("interval"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	events := genEvents(ctx, cls, dur)
	encoder := json.NewEncoder(w)

	w.Header().Set("X-Accel-Buffering", "no")

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			encoder.Encode(event)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			log.Printf("Generated event: %s", event.String())
		}
	}
}

func LaunchEventServer(listener net.Listener) error {
	server := &http.Server{
		Handler: &EventsHandler{},
	}
	return server.Serve(listener)
}

func genEvents(ctx context.Context, class string, intv time.Duration) chan *Event {
	outC := make(chan *Event)
	go func(ctx context.Context) {
		defer close(outC)

		seq := 0
		for {

			req := &Event{
				Class: class,
				Seq:   seq,
				Date:  time.Now(),
			}
			seq++

			select {
			case <-ctx.Done():
				return
			case outC <- req:
				time.Sleep(intv)
				continue
			}
		}
	}(ctx)

	return outC
}

func consumeRemoteEVStream(ctx context.Context, rateLimiter pkgratelimit.RateLimiter, endpoint string, cls string, intv string) {
	inC, outC, errCh := rateLimiter.GetIO(ctx)

	go func() {
		defer close(inC)

		urlObj, err := url.Parse(endpoint)
		if err != nil {
			log.Fatalf("failed to parse endpoint: %v", err)
		}

		querys := url.Values{}
		querys.Set("class", cls)
		if cls == "" {
			log.Fatalf("class is required")
		}
		querys.Set("interval", intv)
		if intv == "" {
			log.Fatalf("interval is required")
		}
		urlObj.RawQuery = querys.Encode()

		log.Printf("Will consume remote event stream from %s", urlObj.String())

		client := &http.Client{}
		req, err := http.NewRequestWithContext(ctx, "GET", urlObj.String(), nil)
		if err != nil {
			log.Fatalf("failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("failed to send request: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			log.Fatalf("failed to get remote event stream: %s", resp.Status)
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			evObj := &Event{}
			err := json.Unmarshal(line, evObj)
			if err != nil {
				log.Printf("failed to unmarshal event: %v", err)
				return
			}
			log.Printf("Received event: %s", evObj.String())
			inC <- evObj
		}
	}()

	for {
		select {
		case err, ok := <-errCh:
			if ok && err != nil {
				log.Printf("error from req class A: %v", err)
			}
			return
		case req, ok := <-outC:
			if !ok {
				log.Printf("output channel from req class A closed")
				return
			}
			log.Printf("Consumed event: %s", req.(*Event).String())
		}
	}
}

func main() {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("failed to listen on a random port: %v", err)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		log.Fatalf("failed to cast listener address to *net.TCPAddr")
	}

	log.Printf("Listening on %s", tcpAddr.String())

	go func() {
		err = LaunchEventServer(listener)
		if err != nil {
			log.Printf("failed to serve http: %v", err)
		}
	}()

	refreshIntv, _ := time.ParseDuration("5s")
	numTokensPerPeriod := 10
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ratelimitPool := &pkgratelimit.MemoryBasedRateLimitPool{
		RefreshIntv:     refreshIntv,
		NumTokensPerKey: numTokensPerPeriod,
	}
	ratelimitPool.Run(ctx)

	// since all requests yield the same key, so this is considered as a globally shared rate limiter
	globalSharedRL := &pkgratelimit.MemoryBasedRateLimiter{
		Pool: ratelimitPool,
		GetKey: func(ctx context.Context, obj interface{}) (string, error) {
			return "", nil
		},
	}

	classes := []string{"A", "B", "C"}
	for _, cls := range classes {
		intv := "50ms"
		go consumeRemoteEVStream(ctx, globalSharedRL, fmt.Sprintf("http://localhost:%d", tcpAddr.Port), cls, intv)
		log.Printf("Started consuming remote event stream for class %s, intv=%s", cls, intv)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Printf("Got signal %s, shutting down ...", sig.String())
}
