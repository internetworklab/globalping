package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgthrottle "example.com/rbmq-demo/pkg/throttle"
	pkgutils "example.com/rbmq-demo/pkg/utils"
)

type AgentCmd struct {
	NodeName      string `help:"Nodename to advertise to the hub, leave it empty for not advertising itself to the hub"`
	HttpEndpoint  string `help:"HTTP endpoint to advertise to the hub"`
	ServerAddress string `help:"WebSocket Address of the hub" default:"wss://hub.example.com:8080/ws"`

	// PeerCAs are use to verify certs presented by the peer,
	// For agent, the peer is the hub, for hub, the peer is the agent.
	// Simply put, PeerCAs are what agent is use to verify the hub's cert, and what hub is use to verify the agent's cert.
	PeerCAs []string `help:"PeerCAs are custom CAs use to verify the hub (server)'s certificate, if none is provided, will use the system CAs to do so. PeerCAs are also use to verify the client's certificate when functioning as a server." type:"path"`

	// Agent will connect to the hub (sometimes), so this is the TLS name (mostly CN field or DNS Alt Name) of the hub.
	ServerName string `help:"Also use to verify the server's certificate"`

	// When the agent is connecting to the hub, the hub needs to authenticate the client, so the client (the agent) also have to present a cert
	// to complete the m-TLS authentication process.
	ClientCert    string `help:"The path to the client certificate" type:"path"`
	ClientCertKey string `help:"The path to the client certificate key" type:"path"`

	// Agent also functions as a server (i.e. provides public tls-secured endpoint, so it might also needs a cert pair)
	ServerCert    string `help:"The path to the server certificate" type:"path"`
	ServerCertKey string `help:"The path to the server key" type:"path"`

	TLSListenAddress string `help:"Address to listen on for TLS" default:"localhost:8081"`
	SharedQuota      int    `help:"Shared quota for the traceroute (packets per second)" default:"10"`
}

type PingHandler struct {
}

func NewPingHandler() *PingHandler {
	ph := new(PingHandler)
	return ph
}

func (ph *PingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pingRequest, err := pkgpinger.ParseSimplePingRequest(r)
	if err != nil {
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: err.Error()})
		return
	}
	pingReqJSB, _ := json.Marshal(pingRequest)
	log.Printf("Started ping request for %s: %s", pkgutils.GetRemoteAddr(r), string(pingReqJSB))
	ctx := r.Context()

	pinger := pkgpinger.NewSimplePinger(pkgpinger.SimplePingerConfig{
		PingRequest: pingRequest,
	})
	for ev := range pinger.Ping(ctx) {
		if ev.Error != nil {
			errStr := ev.Error.Error()
			ev.Err = &errStr
		}
		json.NewEncoder(w).Encode(ev)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

}

func (agentCmd *AgentCmd) Run() error {

	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var customCAs *x509.CertPool = nil
	if agentCmd.PeerCAs != nil {
		customCAs = x509.NewCertPool()
		for _, ca := range agentCmd.PeerCAs {
			log.Printf("Appending CA file to the trust list: %s", ca)
			caData, err := os.ReadFile(ca)
			if err != nil {
				log.Fatalf("failed to read CA file %s: %v", ca, err)
			}
			customCAs.AppendCertsFromPEM(caData)
		}
	}

	if agentCmd.SharedQuota < 1 {
		log.Fatalf("shared quota must be greater than 0")
	}

	throttleConfig := pkgthrottle.TokenBasedThrottleConfig{
		RefreshInterval:       1 * time.Second,
		TokenQuotaPerInterval: agentCmd.SharedQuota,
	}
	tsSched, err := pkgthrottle.NewTimeSlicedEVLoopSched(&pkgthrottle.TimeSlicedEVLoopSchedConfig{})
	if err != nil {
		log.Fatalf("failed to create time sliced event loop scheduler: %v", err)
	}
	tsSchedRunerr := tsSched.Run(ctx)

	throttle := pkgthrottle.NewTokenBasedThrottle(throttleConfig)
	throttle.Run()

	smoother := pkgthrottle.NewBurstSmoother(time.Duration(1000.0/float64(agentCmd.SharedQuota)) * time.Millisecond)
	smoother.Run()

	handler := NewPingHandler()

	muxer := http.NewServeMux()
	muxer.Handle("/simpleping", handler)

	// TLSConfig to apply when acting as a server (i.e. we provide services, peer calls us)
	serverSideTLSCfg := &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
	}
	if customCAs != nil {
		serverSideTLSCfg.ClientCAs = customCAs
	}
	server := http.Server{
		Handler:   muxer,
		TLSConfig: serverSideTLSCfg,
	}
	if agentCmd.ServerCert != "" && agentCmd.ServerCertKey != "" {
		cert, err := tls.LoadX509KeyPair(agentCmd.ServerCert, agentCmd.ServerCertKey)
		if err != nil {
			log.Fatalf("failed to load server certificate: %v", err)
		}
		if serverSideTLSCfg.Certificates == nil {
			serverSideTLSCfg.Certificates = make([]tls.Certificate, 0)
		}
		serverSideTLSCfg.Certificates = append(serverSideTLSCfg.Certificates, cert)
		log.Printf("Loaded server certificate: %s and key: %s", agentCmd.ServerCert, agentCmd.ServerCertKey)
	}

	listener, err := tls.Listen("tcp", agentCmd.TLSListenAddress, serverSideTLSCfg)
	if err != nil {
		log.Fatalf("failed to listen on address %s: %v", agentCmd.TLSListenAddress, err)
	}
	defer listener.Close()
	log.Printf("Listening on address %s", agentCmd.TLSListenAddress)

	go func() {
		if err := server.Serve(listener); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Fatalf("failed to serve: %v", err)
			}
			log.Println("Server exitted")
		}
		go func() {
			<-ctx.Done()
			log.Println("Shutting down server")
			server.Shutdown(ctx)
		}()
	}()

	if agentCmd.NodeName != "" {
		log.Printf("Will advertise self as: %s endpoint: %s hub: %s", agentCmd.NodeName, agentCmd.HttpEndpoint, agentCmd.ServerAddress)
		attributes := make(pkgconnreg.ConnectionAttributes)
		attributes[pkgnodereg.AttributeKeyPingCapability] = "true"
		attributes[pkgnodereg.AttributeKeyNodeName] = agentCmd.NodeName
		attributes[pkgnodereg.AttributeKeyHttpEndpoint] = agentCmd.HttpEndpoint
		agent := pkgnodereg.NodeRegistrationAgent{
			ServerAddress: agentCmd.ServerAddress,
			NodeName:      agentCmd.NodeName,
			ClientCert:    agentCmd.ClientCert,
			ClientCertKey: agentCmd.ClientCertKey,
		}
		agent.NodeAttributes = attributes
		log.Println("Node attributes will be announced as:", attributes)

		log.Println("Initializing node registration agent...")
		if err = agent.Init(); err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}

		log.Println("Starting node registration agent...")

		agent.CustomCertPool = customCAs
		agent.ServerName = agentCmd.ServerName
		go func() {
			nodeRegAgentErrCh := agent.Run(ctx)
			if err := <-nodeRegAgentErrCh; err != nil {
				log.Fatalf("failed to run node registration agent: %v", err)
			}
		}()
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	log.Printf("Received signal: %v, exiting...", sig.String())
	cancel()

	err = <-tsSchedRunerr
	if err != nil {
		log.Fatalf("failed to run time sliced event loop scheduler: %v", err)
	}

	return nil
}
