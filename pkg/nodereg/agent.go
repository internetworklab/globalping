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
)

const (
	AttributeKeyNodeName           = "NodeName"
	AttributeKeyPingCapability     = "CapabilityPing"
	AttributeKeyHttpEndpoint       = "HttpEndpoint"
	AttributeKeyRespondRange       = "RespondRange"
	AttributeKeyExactLocation      = "ExactLocation"
	AttributeKeyDomainRespondRange = "DomainRespondRange"
	AttributeKeySupportUDP         = "SupportUDP"
)

type NodeRegistrationAgent struct {
	ClientCert     string
	ClientCertKey  string
	ServerAddress  string
	NodeName       string
	CorrelationID  *string
	SeqID          *uint64
	TickInterval   *time.Duration
	intialized     bool
	NodeAttributes pkgconnreg.ConnectionAttributes
	LogEchoReplies bool
	ServerName     string
	CustomCertPool *x509.CertPool
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

	if agent.TickInterval == nil {
		agent.TickInterval = new(time.Duration)
		*agent.TickInterval = 1 * time.Second
		log.Printf("Using default tick interval: %s", *agent.TickInterval)
	}

	agent.intialized = true
	return nil
}

func (agent *NodeRegistrationAgent) runReceiver(c *websocket.Conn) error {
	for {
		msgTy, msg, err := c.ReadMessage()
		if err != nil {
			return fmt.Errorf("failed to read message from %s: %v", c.RemoteAddr(), err)
		}

		switch msgTy {
		case websocket.TextMessage:
			var payload pkgframing.MessagePayload
			err := json.Unmarshal(msg, &payload)
			if err != nil {
				log.Printf("Failed to unmarshal message from %s: %v", c.RemoteAddr(), err)
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

		default:
			log.Printf("Received unknown message type from %s: %d", c.RemoteAddr(), msgTy)
		}
	}
}

func (agent *NodeRegistrationAgent) sendMessage(c *websocket.Conn, msg interface{}) error {
	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}
	err = c.WriteMessage(websocket.TextMessage, jsonMsg)
	if err != nil {
		return fmt.Errorf("failed to write message: %v", err)
	}
	return nil
}

// Connect and start the loop
func (agent *NodeRegistrationAgent) Run(ctx context.Context) chan error {
	errCh := make(chan error)
	go func() {
		errCh <- agent.doRun(ctx)
	}()
	return errCh
}

func (agent *NodeRegistrationAgent) doRun(ctx context.Context) error {
	if !agent.intialized {
		return fmt.Errorf("agent not initialized")
	}

	errCh := make(chan error)

	go func() {
		defer close(errCh)

		systemCertPool, err := x509.SystemCertPool()
		if err != nil {
			errCh <- fmt.Errorf("failed to get system cert pool: %v", err)
			return
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
				errCh <- fmt.Errorf("failed to load client certificate: %v", err)
				return
			}
			if tlsConfig.Certificates == nil {
				tlsConfig.Certificates = make([]tls.Certificate, 0)
			}
			tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
		}

		dialer := &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 45 * time.Second,
			TLSClientConfig:  tlsConfig,
		}

		log.Printf("Agent %s started, connecting to %s", agent.NodeName, agent.ServerAddress)
		c, _, err := dialer.Dial(agent.ServerAddress, nil)
		if err != nil {
			errCh <- fmt.Errorf("failed to dial %s: %v", agent.ServerAddress, err)
			return
		}
		defer c.Close()

		log.Printf("Connected to server %s: remote address: %s", agent.ServerAddress, c.RemoteAddr())

		receiverExit := make(chan error)
		go func() {
			receiverExit <- agent.runReceiver(c)
		}()

		ticker := time.NewTicker(*agent.TickInterval)
		defer ticker.Stop()

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
		log.Printf("Sending register message")
		err = agent.sendMessage(c, registerMsg)
		if err != nil {
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
				msg := pkgframing.MessagePayload{
					Echo: &pkgconnreg.EchoPayload{
						Direction:     pkgconnreg.EchoDirectionC2S,
						CorrelationID: *agent.CorrelationID,
						Timestamp:     uint64(time.Now().UnixMilli()),
						SeqID:         *agent.SeqID,
					},
				}
				nextSeq := *agent.SeqID + 1
				*agent.SeqID = nextSeq

				err := agent.sendMessage(c, msg)
				if err != nil {
					errCh <- fmt.Errorf("failed to send echo message: %v", err)
					return
				}
			}
		}
	}()

	return <-errCh
}
