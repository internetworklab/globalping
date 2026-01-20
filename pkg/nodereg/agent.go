package nodereg

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgframing "example.com/rbmq-demo/pkg/framing"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	quicGo "github.com/quic-go/quic-go"
	quicHttp3 "github.com/quic-go/quic-go/http3"
)

const (
	AttributeKeyNodeName           = "NodeName"
	AttributeKeyPingCapability     = "CapabilityPing"
	AttributeKeyDNSProbeCapability = "CapabilityDNSProbe"
	AttributeKeySupportQUICTunnel  = "SupportQUICTunnel"
	AttributeKeyHttpEndpoint       = "HttpEndpoint"
	AttributeKeyRespondRange       = "RespondRange"
	AttributeKeyExactLocation      = "ExactLocation"
	AttributeKeyDomainRespondRange = "DomainRespondRange"
	AttributeKeySupportUDP         = "SupportUDP"
	AttributeKeySupportPMTU        = "SupportPMTU"
	AttributeKeySupportTCP         = "SupportTCP"
	AttributeKeyVersion            = "Version"
)

type NodeRegistrationAgent struct {
	HTTPMuxer         *http.ServeMux
	ClientCert        string
	ClientCertKey     string
	ServerAddress     string
	QUICServerAddress string
	NodeName          string
	CorrelationID     *string
	SeqID             *uint64
	TickInterval      time.Duration
	intialized        bool
	NodeAttributes    pkgconnreg.ConnectionAttributes
	LogEchoReplies    bool
	ServerName        string
	CustomCertPool    *x509.CertPool
	UseQUIC           bool

	wsConn     *websocket.Conn
	quicConn   *quicGo.Conn
	quicStream *quicGo.Stream
	reader     io.Reader
}

func (agent *NodeRegistrationAgent) Init() error {
	if agent.ServerAddress == "" {
		return fmt.Errorf("server address is required")
	}

	if agent.NodeName == "" {
		return fmt.Errorf("node name is required")
	}

	if agent.CorrelationID == nil {
		corrId := uuid.New().String()
		agent.CorrelationID = &corrId
		log.Printf("Using default correlation ID: %s", corrId)
	}

	if agent.SeqID == nil {
		seqId := uint64(0)
		agent.SeqID = &seqId
		log.Printf("Will start at sequence ID: %d", seqId)
	}

	log.Printf("Agent will use tick interval: %s", agent.TickInterval.String())

	agent.intialized = true
	return nil
}

func (agent *NodeRegistrationAgent) runReceiver() error {
	for {
		var payload pkgframing.MessagePayload
		err := json.NewDecoder(agent.reader).Decode(&payload)
		if err != nil {
			log.Printf("Failed to unmarshal message from remote: %v", err)
			continue
		}

		if payload.Echo != nil &&
			payload.Echo.CorrelationID == *agent.CorrelationID &&
			payload.Echo.Direction == pkgconnreg.EchoDirectionS2C {

			rtt, onTrip, backTrip := payload.Echo.CalculateDelays(time.Now())
			if agent.LogEchoReplies {
				log.Printf("Received echo reply: Seq: %d, RTT: %d ms, On-trip: %d ms, Back-trip: %d ms", payload.Echo.SeqID, rtt.Milliseconds(), onTrip.Milliseconds(), backTrip.Milliseconds())
			}
		}
	}
}

// Connect and start the loop
func (agent *NodeRegistrationAgent) Run(ctx context.Context) chan error {
	errCh := make(chan error)
	go func() {
		errCh <- agent.doRun(ctx)
	}()
	return errCh
}

func (agent *NodeRegistrationAgent) getTLSConfig() (*tls.Config, error) {
	systemCertPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("failed to get system cert pool: %v", err)
	}

	tlsConfig := &tls.Config{
		RootCAs:    systemCertPool,
		ServerName: agent.ServerName,
	}
	if agent.CustomCertPool != nil {
		tlsConfig.RootCAs = agent.CustomCertPool
	}
	if agent.ClientCert != "" && agent.ClientCertKey != "" {
		cert, err := tls.LoadX509KeyPair(agent.ClientCert, agent.ClientCertKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %v", err)
		}
		if tlsConfig.Certificates == nil {
			tlsConfig.Certificates = make([]tls.Certificate, 0)
		}
		tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
	}
	return tlsConfig, nil
}

func (agent *NodeRegistrationAgent) connectWs(tlsConfig *tls.Config) (*websocket.Conn, error) {
	log.Printf("Agent %s started, connecting to %s", agent.NodeName, agent.ServerAddress)
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
		TLSClientConfig:  tlsConfig,
	}

	c, _, err := dialer.Dial(agent.ServerAddress, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %v", agent.ServerAddress, err)
	}

	log.Printf("Connected to server %s: remote address: %s", agent.ServerAddress, c.RemoteAddr())
	return c, nil
}

func (agent *NodeRegistrationAgent) getRegisterPayload() pkgframing.MessagePayload {
	log.Printf("Using node name: %s", agent.NodeName)
	registerPayload := pkgconnreg.RegisterPayload{
		NodeName: agent.NodeName,
	}
	registerMsg := pkgframing.MessagePayload{
		Register: &registerPayload,
	}
	if agent.NodeAttributes != nil {
		registerMsg.AttributesAnnouncement = &pkgconnreg.AttributesAnnouncementPayload{
			Attributes: agent.NodeAttributes,
		}
		s, _ := json.Marshal(registerMsg.AttributesAnnouncement)
		log.Printf("Will announcing attributes: %+v", string(s))
	}
	return registerMsg
}

func (agent *NodeRegistrationAgent) getTickMsg() (pkgframing.MessagePayload, uint64) {
	msg := pkgframing.MessagePayload{
		Echo: &pkgconnreg.EchoPayload{
			Direction:     pkgconnreg.EchoDirectionC2S,
			CorrelationID: *agent.CorrelationID,
			Timestamp:     uint64(time.Now().UnixMilli()),
			SeqID:         *agent.SeqID,
		},
	}
	nextSeq := *agent.SeqID + 1
	return msg, nextSeq
}

func (agent *NodeRegistrationAgent) getWriter() (io.Writer, error) {
	if agent.UseQUIC {
		return agent.quicStream, nil
	}
	return agent.wsConn.NextWriter(websocket.TextMessage)
}

func (agent *NodeRegistrationAgent) writeMsg(msg interface{}) error {
	w, err := agent.getWriter()
	if err != nil {
		return fmt.Errorf("failed to get next writer: %v", err)
	}
	return json.NewEncoder(w).Encode(msg)
}

func (agent *NodeRegistrationAgent) doRun(ctx context.Context) error {
	if !agent.intialized {
		return fmt.Errorf("agent not initialized")
	}

	errCh := make(chan error)

	go func() {
		registerMsg := agent.getRegisterPayload()

		defer close(errCh)

		tlsConfig, err := agent.getTLSConfig()
		if err != nil {
			errCh <- fmt.Errorf("failed to get TLS config: %v", err)
			return
		}

		if agent.UseQUIC {
			tlsConfig.NextProtos = []string{"h3"}
			quicConn, err := quicGo.DialAddr(ctx, agent.QUICServerAddress, tlsConfig, nil)
			if err != nil {
				errCh <- fmt.Errorf("failed to dial QUIC address %s: %v", agent.QUICServerAddress, err)
				return
			}
			agent.quicConn = quicConn
			log.Printf("Dialed QUIC address: %s,", quicConn.RemoteAddr())

			stream, err := agent.quicConn.OpenStreamSync(ctx)
			if err != nil {
				errCh <- fmt.Errorf("failed to open QUIC stream: %v", err)
				return
			}
			agent.quicStream = stream
			agent.reader = stream

			server := &quicHttp3.Server{
				Handler: agent.HTTPMuxer,
			}
			rawServerConn, err := server.NewRawServerConn(agent.quicConn)
			if err != nil {
				log.Printf("failed to create raw server connection from quic %p: %v", agent.quicConn.RemoteAddr(), err)
				return
			}
			log.Printf("Created raw server connection from quic %p", agent.quicConn.RemoteAddr())

			go func() {
				defer log.Printf("QUIC proxy exitting")
				for {
					stream, err := agent.quicConn.AcceptStream(ctx)
					if err != nil {
						log.Printf("failed to accept QUIC stream from conn %s: %v", agent.quicConn.RemoteAddr(), err)
						return
					}
					log.Printf("Accepted QUIC stream from conn %p", agent.quicConn.RemoteAddr())
					go func(stream *quicGo.Stream) {
						defer stream.Close()
						defer log.Printf("QUIC stream %d of conn %p exitting", stream.StreamID(), agent.quicConn.RemoteAddr())

						log.Printf("Handling request stream %d of conn %p", stream.StreamID(), agent.quicConn.RemoteAddr())
						rawServerConn.HandleRequestStream(stream)
					}(stream)
				}
			}()
		} else {
			c, err := agent.connectWs(tlsConfig)
			if err != nil {
				errCh <- fmt.Errorf("failed to connect to WS: %v", err)
				return
			}
			agent.wsConn = c
			_, agent.reader, err = c.NextReader()
		}

		receiverExit := make(chan error)
		go func() {
			receiverExit <- agent.runReceiver()
		}()

		ticker := time.NewTicker(agent.TickInterval)
		defer ticker.Stop()

		log.Printf("Sending register message")
		if err := agent.writeMsg(registerMsg); err != nil {
			errCh <- fmt.Errorf("failed to send register message: %v", err)
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case receiverErr := <-receiverExit:
				var err error
				if receiverErr != nil {
					err = fmt.Errorf("receiver exited with error: %v", receiverErr)
				}
				errCh <- err
				return
			case <-ticker.C:
				var msg pkgframing.MessagePayload
				msg, *agent.SeqID = agent.getTickMsg()
				if err := agent.writeMsg(msg); err != nil {
					errCh <- fmt.Errorf("failed to send echo message: %v", err)
					return
				}
			}
		}
	}()

	return <-errCh
}
