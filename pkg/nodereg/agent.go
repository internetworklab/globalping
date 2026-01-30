package nodereg

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
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
	AttributeKeyCountryCode        = "CountryCode"
	AttributeKeyCityName           = "CityName"
	AttributeKeyASN                = "ProviderASN"
	AttributeKeyISP                = "ProviderName"
	AttributeKeyDN42ASN            = "DN42ProviderASN"
	AttributeKeyDN42ISP            = "DN42ProviderName"
	AttributeKeyDomainRespondRange = "DomainRespondRange"
	AttributeKeySupportUDP         = "SupportUDP"
	AttributeKeySupportPMTU        = "SupportPMTU"
	AttributeKeySupportTCP         = "SupportTCP"
	AttributeKeyVersion            = "Version"
)

type NodeRegistrationAgent struct {
	HTTPMuxer         http.Handler
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
		payload, err := agent.recvMsgPayload()
		if err != nil {
			return fmt.Errorf("failed to receive message from remote: %v", err)
		}

		if payload != nil && payload.Echo != nil &&
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
			quicRAddr := quicConn.RemoteAddr()
			log.Printf("Dialed QUIC address: %s,", quicRAddr)

			stream, err := agent.quicConn.OpenStreamSync(ctx)
			if err != nil {
				errCh <- fmt.Errorf("failed to open QUIC stream for %s: %v", quicRAddr, err)
				return
			}
			agent.quicStream = stream

			server := &quicHttp3.Server{
				Handler: agent.HTTPMuxer,
			}
			rawServerConn, err := server.NewRawServerConn(agent.quicConn)
			if err != nil {
				log.Printf("failed to create raw server connection from quic %s: %v", quicRAddr, err)
				return
			}
			log.Printf("Created raw server connection from quic %s", quicRAddr)

			go func() {
				defer log.Printf("QUIC proxy is exitting")
				for {
					stream, err := agent.quicConn.AcceptStream(ctx)
					if err != nil {
						log.Printf("failed to accept QUIC stream from conn %s: %v", quicRAddr, err)
						return
					}
					log.Printf("Accepted QUIC stream %d from conn %s", stream.StreamID(), quicRAddr)
					go func(stream *quicGo.Stream) {
						defer stream.Close()
						defer log.Printf("QUIC stream %d of conn %s exitting", stream.StreamID(), quicRAddr)

						log.Printf("Handling request stream %d of conn %s", stream.StreamID(), quicRAddr)
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
		}

		receiverExit := make(chan error)
		go func() {
			receiverExit <- agent.runReceiver()
		}()

		ticker := time.NewTicker(agent.TickInterval)
		defer ticker.Stop()

		log.Printf("Sending register message")
		if err := agent.sendMsgPayload(&registerMsg); err != nil {
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
				if err := agent.sendMsgPayload(&msg); err != nil {
					errCh <- fmt.Errorf("failed to send echo message: %v", err)
					return
				}
			}
		}
	}()

	return <-errCh
}

func (agent *NodeRegistrationAgent) sendMsgPayload(payload *pkgframing.MessagePayload) error {
	if agent.UseQUIC {
		if agent.quicStream == nil {
			panic("quic stream shouldn't be nil when using QUIC")
		}
		return json.NewEncoder(agent.quicStream).Encode(payload)
	}

	if agent.wsConn == nil {
		panic("ws conn shouldn't be nil when using WebSocket")
	}

	return agent.wsConn.WriteJSON(payload)
}

func (agent *NodeRegistrationAgent) recvMsgPayload() (*pkgframing.MessagePayload, error) {
	var payload pkgframing.MessagePayload
	if agent.UseQUIC {
		if agent.quicStream == nil {
			panic("quic stream shouldn't be nil when using QUIC")
		}

		err := json.NewDecoder(agent.quicStream).Decode(&payload)
		if err != nil {
			return nil, fmt.Errorf("failed to read or decode message from QUIC stream: %v", err)
		}
		return &payload, nil
	}

	if agent.wsConn == nil {
		panic("ws conn shouldn't be nil when using WebSocket")
	}

	err := agent.wsConn.ReadJSON(&payload)
	if err != nil {
		return nil, fmt.Errorf("failed to read or decode message from WebSocket: %v", err)
	}

	return &payload, nil
}
