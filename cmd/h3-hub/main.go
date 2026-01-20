package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"log"
	"net"

	quicGo "github.com/quic-go/quic-go"
)

type MyCtxKey string

const (
	MyCtxKeyConn = MyCtxKey("conn")
)

func main() {
	certPath := "/root/services/globalping/agent/certs/peer.pem"
	certKeyPath := "/root/services/globalping/agent/certs/peer-key.pem"
	certPair, err := tls.LoadX509KeyPair(certPath, certKeyPath)
	if err != nil {
		log.Fatalf("failed to load TLS certificate: %v", err)
	}
	log.Printf("Loaded TLS certificate: %s and key: %s", certPath, certKeyPath)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certPair},
		NextProtos:   []string{"h3"},
	}
	h3Listener, err := quicGo.ListenAddr("0.0.0.0:18443", tlsConfig, nil)
	if err != nil {
		log.Fatalf("failed to listen on address 0.0.0.0:18443: %v", err)
	}
	udpAddr, ok := h3Listener.Addr().(*net.UDPAddr)
	if !ok {
		panic("failed to cast listener address to *net.UDPAddr")
	}
	log.Printf("Listening on UDP address %s", udpAddr.String())

	for {
		conn, err := h3Listener.Accept(context.Background())
		if err != nil {
			log.Fatalf("failed to accept connection: %v", err)
		}

		log.Printf("Accepted connection: %p %s", conn, conn.RemoteAddr())

		go func(conn *quicGo.Conn) {
			defer log.Printf("Closing connection: %p %s", conn, conn.RemoteAddr())
			for {
				log.Printf("Accepting stream from connection: %p %s", conn, conn.RemoteAddr())
				stream, err := conn.AcceptStream(context.Background())
				if err != nil {
					log.Printf("failed to accept stream: %v", err)
					break
				}
				log.Printf("Accepted stream: %p %d from connection: %s", stream, stream.StreamID(), conn.RemoteAddr())

				go func(stream *quicGo.Stream) {
					defer stream.Close()
					defer log.Printf("Closing stream: %p %d", stream, stream.StreamID())

					for {
						scanner := bufio.NewScanner(stream)
						for scanner.Scan() {
							line := scanner.Text()
							log.Printf("Received line: %s", line)
						}
						if err := scanner.Err(); err != nil {
							log.Printf("failed to scan stream: %v", err)
							break
						}
					}
				}(stream)
			}
		}(conn)
	}
}
