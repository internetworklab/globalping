package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkghandler "example.com/rbmq-demo/pkg/handler"
	pkgsafemap "example.com/rbmq-demo/pkg/safemap"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

const serverShutdownTimeout = 30 * time.Second

type HubCmd struct {
	PeerCAs       []string `help:"A list of path to the CAs use to verify peer certificates, can be specified multiple times" type:"path"`
	Address       string   `help:"The address to listen on" default:"localhost:8080"`
	WebSocketPath string   `help:"The path to the WebSocket endpoint" default:"/ws"`

	// When the hub is calling functions exposed by the agent, it have to authenticate itself to the agent.
	ClientCert    string `help:"The path to the client certificate" type:"path"`
	ClientCertKey string `help:"The path to the client certificate key" type:"path"`

	// Certificates to present to the clients when the hub itself is acting as a server.
	ServerCert    string `help:"The path to the server certificate" type:"path"`
	ServerCertKey string `help:"The path to the server certificate key" type:"path"`
}

func (hubCmd HubCmd) Run() error {

	var customCAs *x509.CertPool = nil
	if hubCmd.PeerCAs != nil {
		customCAs = x509.NewCertPool()
		for _, ca := range hubCmd.PeerCAs {
			log.Printf("Appending CA file to the trust list: %s", ca)
			caData, err := os.ReadFile(ca)
			if err != nil {
				log.Fatalf("Failed to read CA file %s: %v", ca, err)
			}
			customCAs.AppendCertsFromPEM(caData)
		}
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sm := pkgsafemap.NewSafeMap()
	cr := pkgconnreg.NewConnRegistry(sm)

	wsHandler := pkghandler.NewWebsocketHandler(&upgrader, cr)
	connsHandler := pkghandler.NewConnsHandler(cr)
	var clientTLSConfig *tls.Config = &tls.Config{}
	if customCAs != nil {
		clientTLSConfig.ClientCAs = customCAs
	}
	if hubCmd.ClientCert != "" && hubCmd.ClientCertKey != "" {
		cert, err := tls.LoadX509KeyPair(hubCmd.ClientCert, hubCmd.ClientCertKey)
		if err != nil {
			log.Fatalf("Failed to load server certificate: %v", err)
		}
		if clientTLSConfig.Certificates == nil {
			clientTLSConfig.Certificates = make([]tls.Certificate, 0)
		}
		clientTLSConfig.Certificates = append(clientTLSConfig.Certificates, cert)
		log.Printf("Loaded client certificate: %s and key: %s", hubCmd.ClientCert, hubCmd.ClientCertKey)
	}
	pingHandler := &pkghandler.PingTaskHandler{
		ConnRegistry:    cr,
		ClientTLSConfig: clientTLSConfig,
	}

	muxer := http.NewServeMux()
	muxer.Handle(hubCmd.WebSocketPath, wsHandler)
	muxer.Handle("/conns", connsHandler)
	muxer.Handle("/ping", pingHandler)

	certPool, err := x509.SystemCertPool()
	if err != nil {
		log.Fatalf("Failed to get system cert pool: %v", err)
	}

	// TLSConfig when functioning as a server (i.e. we are the server, while the peer is the client)
	serverSideTLSCfg := &tls.Config{
		ClientAuth:         tls.RequireAndVerifyClientCert,
		ClientCAs:          certPool,
		InsecureSkipVerify: false,
	}
	if hubCmd.ServerCert != "" && hubCmd.ServerCertKey != "" {
		cert, err := tls.LoadX509KeyPair(hubCmd.ServerCert, hubCmd.ServerCertKey)
		if err != nil {
			log.Fatalf("Failed to load server certificate: %v", err)
		}
		if serverSideTLSCfg.Certificates == nil {
			serverSideTLSCfg.Certificates = make([]tls.Certificate, 0)
		}
		serverSideTLSCfg.Certificates = append(serverSideTLSCfg.Certificates, cert)
		log.Printf("Loaded server certificate: %s and key: %s", hubCmd.ServerCert, hubCmd.ServerCertKey)
	}
	if customCAs != nil {
		serverSideTLSCfg.ClientCAs = customCAs
	}

	server := http.Server{
		Handler: pkghandler.NewWithCORSHandler(muxer),
	}

	listener, err := tls.Listen("tcp", hubCmd.Address, serverSideTLSCfg)
	if err != nil {
		log.Fatalf("Failed to listen on address %s: %v", hubCmd.Address, err)
	}
	log.Printf("Listening on %s", listener.Addr())

	go func() {
		log.Printf("Starting server on %s", listener.Addr())
		err = server.Serve(listener)
		if err != nil {
			if err != http.ErrServerClosed {
				log.Fatalf("Failed to serve: %v", err)
			}
		}
	}()

	<-sigs
	log.Println("Shutting down safe map...")
	sm.Close()
	log.Println("Safe map shut down successfully")

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()
	err = server.Shutdown(ctx)
	if err != nil {
		log.Printf("Failed to shutdown server: %v", err)
	}
	log.Println("Server shut down successfully")
	return nil
}
