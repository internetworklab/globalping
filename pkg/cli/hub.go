package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkghandler "example.com/rbmq-demo/pkg/handler"
	pkgsafemap "example.com/rbmq-demo/pkg/safemap"
	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

type HubCmd struct {
	PeerCAs       []string `help:"A list of path to the CAs use to verify peer certificates, can be specified multiple times" type:"path"`
	Address       string   `help:"The address to listen on for private operations" default:":8080"`
	AddressPublic string   `help:"The address to listen on for public operations" default:":8082"`
	WebSocketPath string   `help:"The path to the WebSocket endpoint" default:"/ws"`

	// When the hub is calling functions exposed by the agent, it have to authenticate itself to the agent.
	ClientCert    string `help:"The path to the client certificate" type:"path"`
	ClientCertKey string `help:"The path to the client certificate key" type:"path"`

	// Certificates to present to the clients when the hub itself is acting as a server.
	ServerCert    string `help:"The path to the server certificate" type:"path"`
	ServerCertKey string `help:"The path to the server certificate key" type:"path"`
}

func (hubCmd HubCmd) Run() error {

	customCAs, err := pkgutils.NewCustomCAPool(hubCmd.PeerCAs)
	if err != nil {
		log.Fatalf("Failed to create custom CA pool: %v", err)
	} else if len(hubCmd.PeerCAs) > 0 {
		log.Printf("Appended custom CAs: %s", strings.Join(hubCmd.PeerCAs, ", "))
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sm := pkgsafemap.NewSafeMap()
	cr := pkgconnreg.NewConnRegistry(sm)

	wsHandler := pkghandler.NewWebsocketHandler(&upgrader, cr)
	connsHandler := pkghandler.NewConnsHandler(cr)
	var clientTLSConfig *tls.Config = &tls.Config{}
	if customCAs != nil {
		clientTLSConfig.RootCAs = customCAs
	}
	if hubCmd.ClientCert != "" && hubCmd.ClientCertKey != "" {
		cert, err := tls.LoadX509KeyPair(hubCmd.ClientCert, hubCmd.ClientCertKey)
		if err != nil {
			log.Fatalf("Failed to load client certificate: %v", err)
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

	// muxerPrivate is for privileged rw operations
	muxerPrivate := http.NewServeMux()
	muxerPrivate.Handle(hubCmd.WebSocketPath, wsHandler)

	// muxerPublic is for public low-privileged operations
	muxerPublic := http.NewServeMux()
	muxerPublic.Handle("/conns", connsHandler)
	muxerPublic.Handle("/ping", pingHandler)

	certPool, err := x509.SystemCertPool()
	if err != nil {
		log.Fatalf("Failed to get system cert pool: %v", err)
	}

	// TLSConfig when functioning as a server (i.e. we are the server, while the peer is the client)
	privateServerSideTLSCfg := &tls.Config{
		ClientAuth:         tls.RequireAndVerifyClientCert,
		ClientCAs:          certPool,
		InsecureSkipVerify: false,
	}
	if hubCmd.ServerCert != "" && hubCmd.ServerCertKey != "" {
		cert, err := tls.LoadX509KeyPair(hubCmd.ServerCert, hubCmd.ServerCertKey)
		if err != nil {
			log.Fatalf("Failed to load server certificate: %v", err)
		}
		if privateServerSideTLSCfg.Certificates == nil {
			privateServerSideTLSCfg.Certificates = make([]tls.Certificate, 0)
		}
		privateServerSideTLSCfg.Certificates = append(privateServerSideTLSCfg.Certificates, cert)
		log.Printf("Loaded server certificate: %s and key: %s", hubCmd.ServerCert, hubCmd.ServerCertKey)
	}
	if customCAs != nil {
		privateServerSideTLSCfg.ClientCAs = customCAs
	}

	privateServer := http.Server{
		Handler: pkghandler.NewWithCORSHandler(muxerPrivate),
	}
	publicServer := http.Server{
		Handler: pkghandler.NewWithCORSHandler(muxerPublic),
	}

	privateListener, err := tls.Listen("tcp", hubCmd.Address, privateServerSideTLSCfg)
	if err != nil {
		log.Fatalf("Failed to listen on address %s: %v", hubCmd.Address, err)
	}
	log.Printf("Listening on %s for private operations", hubCmd.Address)

	publicListener, err := net.Listen("tcp", hubCmd.AddressPublic)
	if err != nil {
		log.Fatalf("Failed to listen on address %s: %v", hubCmd.AddressPublic, err)
	}
	log.Printf("Listening on %s for public operations", hubCmd.AddressPublic)

	go func() {
		log.Printf("Starting private server on %s", privateListener.Addr())
		err = privateServer.Serve(privateListener)
		if err != nil {
			if err != http.ErrServerClosed {
				log.Fatalf("Failed to serve: %v", err)
			}
		}
	}()

	go func() {
		log.Printf("Starting public server on %s", publicListener.Addr())
		err = publicServer.Serve(publicListener)
		if err != nil {
			if err != http.ErrServerClosed {
				log.Fatalf("Failed to serve: %v", err)
			}
		}
	}()

	sig := <-sigs
	log.Printf("Received %s, shutting down ...", sig.String())
	sm.Close()

	log.Println("Shutting down private server...")
	err = privateServer.Shutdown(context.TODO())
	if err != nil {
		log.Printf("Failed to shutdown server: %v", err)
	}

	log.Println("Shutting down public server...")
	err = publicServer.Shutdown(context.TODO())
	if err != nil {
		log.Printf("Failed to shutdown public server: %v", err)
	}

	return nil
}
