package raw

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type GeneralICMPTransceiver interface {
	GetSender() chan<- ICMPSendRequest
	GetReceiver() chan<- chan ICMPReceiveReply
}

type ICMP4TransceiverConfig struct {
	// ICMP ID to use
	ID int
}

type ICMPSendRequest struct {
	Dst net.IPAddr
	Seq int
	TTL int
}

type ICMPReceiveReply struct {
	ID   int
	Size int
	Seq  int
	TTL  int

	// it's basically the same as Addr.Network() + ':' + Addr.String()
	Peer string

	ReceivedAt time.Time

	ICMPTypeV4 *ipv4.ICMPType
	ICMPTypeV6 *ipv6.ICMPType
}

type ICMP4Transceiver struct {
	id             int
	packetConn     net.PacketConn
	ipv4PacketConn *ipv4.PacketConn

	// User send requests to here, we retrieve the request,
	// then we translate it to the wire format.
	SendC chan ICMPSendRequest

	// ICMP replies
	ReceiveC chan chan ICMPReceiveReply
}

func NewICMP4Transceiver(config ICMP4TransceiverConfig) (*ICMP4Transceiver, error) {

	tracer := &ICMP4Transceiver{
		id:       config.ID,
		SendC:    make(chan ICMPSendRequest),
		ReceiveC: make(chan chan ICMPReceiveReply),
	}

	return tracer, nil
}

func getMaximumMTU() int {
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	maximumMTU := -1
	for _, iface := range ifaces {
		if iface.MTU > maximumMTU {
			maximumMTU = iface.MTU
		}
	}
	if maximumMTU == -1 {
		panic("can't determine maximum MTU")
	}
	return maximumMTU
}

func (icmp4tr *ICMP4Transceiver) Run(ctx context.Context) error {
	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("failed to listen on packet:icmp: %v", err)
	}
	go func() {
		defer conn.Close()

		icmp4tr.packetConn = conn
		icmp4tr.ipv4PacketConn = ipv4.NewPacketConn(conn)

		if err := icmp4tr.ipv4PacketConn.SetControlMessage(ipv4.FlagTTL|ipv4.FlagSrc|ipv4.FlagDst|ipv4.FlagInterface, true); err != nil {
			log.Fatal(err)
		}

		wm := icmp.Message{
			Type: ipv4.ICMPTypeEcho, Code: 0,
			Body: &icmp.Echo{
				ID:   icmp4tr.id,
				Data: nil,
			},
		}

		bufSize := getMaximumMTU()
		rb := make([]byte, bufSize)

		// launch receiving goroutine
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case replysSubCh := <-icmp4tr.ReceiveC:
					for {
						nBytes, ctrlMsg, peerAddr, err := icmp4tr.ipv4PacketConn.ReadFrom(rb)
						if err != nil {
							if err, ok := err.(net.Error); ok && err.Timeout() {
								continue
							}
							log.Printf("failed to read from connection: %v", err)
							break
						}
						receiveMsg, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), rb[:nBytes])
						if err != nil {
							log.Fatalf("failed to parse icmp message: %v", err)
						}

						receivedAt := time.Now()
						replyObject := ICMPReceiveReply{
							ID:         icmp4tr.id,
							Size:       nBytes,
							ReceivedAt: receivedAt,
							Peer:       peerAddr.Network() + ":" + peerAddr.String(),
							TTL:        ctrlMsg.TTL,
							Seq:        -1, // if can't determine, use -1
						}

						icmpBody, ok := receiveMsg.Body.(*icmp.Echo)
						if !ok {
							log.Printf("failed to parse icmp body: %+v", receiveMsg)
							continue
						}

						if icmpBody.ID != icmp4tr.id {
							// silently ignore the message that is not for us
							continue
						}

						replyObject.Seq = icmpBody.Seq
						var ty ipv4.ICMPType

						switch receiveMsg.Type {
						case ipv4.ICMPTypeTimeExceeded:
							ty = ipv4.ICMPTypeTimeExceeded
							replyObject.ICMPTypeV4 = &ty
						case ipv4.ICMPTypeEchoReply:
							ty = ipv4.ICMPTypeEchoReply
						default:
							log.Printf("unknown ICMP message: %+v", receiveMsg)
							continue
						}

						replyObject.ICMPTypeV4 = &ty
						replysSubCh <- replyObject
						break
					}
				}
			}
		}()

		// launch sending goroutine
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case req := <-icmp4tr.SendC:
					wm.Body.(*icmp.Echo).Seq = req.Seq
					wb, err := wm.Marshal(nil)
					if err != nil {
						log.Fatalf("failed to marshal icmp message: %v", err)
					}

					if err := icmp4tr.ipv4PacketConn.SetTTL(req.TTL); err != nil {
						log.Fatalf("failed to set TTL: %v", err)
					}

					dst := req.Dst
					if _, err := icmp4tr.ipv4PacketConn.WriteTo(wb, nil, &dst); err != nil {
						log.Fatalf("failed to write to connection: %v", err)
					}
				}
			}
		}()

		<-ctx.Done()
	}()

	return nil
}

func (icmp4tr *ICMP4Transceiver) GetSender() chan<- ICMPSendRequest {
	return icmp4tr.SendC
}

func (icmp4tr *ICMP4Transceiver) GetReceiver() chan<- chan ICMPReceiveReply {
	return icmp4tr.ReceiveC
}

type ICMP6TransceiverConfig struct {
	// ICMP ID to use
	ID int
}

type ICMP6Transceiver struct {
	id             int
	packetConn     net.PacketConn
	ipv6PacketConn *ipv6.PacketConn

	// User send requests to here, we retrieve the request,
	// then we translate it to the wire format.
	SendC chan ICMPSendRequest

	// ICMP replies
	ReceiveC chan chan ICMPReceiveReply
}

func NewICMP6Transceiver(config ICMP6TransceiverConfig) (*ICMP6Transceiver, error) {

	tracer := &ICMP6Transceiver{
		id:       config.ID,
		SendC:    make(chan ICMPSendRequest),
		ReceiveC: make(chan chan ICMPReceiveReply),
	}

	return tracer, nil
}

func (icmp6tr *ICMP6Transceiver) Run(ctx context.Context) error {
	conn, err := net.ListenPacket("ip6:58", "::")
	if err != nil {
		return fmt.Errorf("failed to listen on packet:ip6-icmp: %v", err)
	}
	go func() {
		defer conn.Close()

		icmp6tr.packetConn = conn
		icmp6tr.ipv6PacketConn = ipv6.NewPacketConn(conn)

		if err := icmp6tr.ipv6PacketConn.SetControlMessage(ipv6.FlagHopLimit|ipv6.FlagSrc|ipv6.FlagDst|ipv6.FlagInterface, true); err != nil {
			log.Fatal(err)
		}

		wm := icmp.Message{
			Type: ipv6.ICMPTypeEchoRequest, Code: 0,
			Body: &icmp.Echo{
				ID:   icmp6tr.id,
				Data: nil,
			},
		}

		var f ipv6.ICMPFilter
		f.SetAll(true)
		f.Accept(ipv6.ICMPTypeTimeExceeded)
		f.Accept(ipv6.ICMPTypeEchoReply)
		if err := icmp6tr.ipv6PacketConn.SetICMPFilter(&f); err != nil {
			log.Fatal(err)
		}

		var wcm ipv6.ControlMessage
		bufSize := getMaximumMTU()
		rb := make([]byte, bufSize)

		// launch receiving goroutine
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case replySubCh := <-icmp6tr.ReceiveC:
					for {
						nBytes, ctrlMsg, peerAddr, err := icmp6tr.ipv6PacketConn.ReadFrom(rb)
						if err != nil {
							if err, ok := err.(net.Error); ok && err.Timeout() {
								log.Printf("timeout reading from connection, skipping")
								continue
							}
							log.Printf("failed to read from connection: %v", err)
							break
						}
						receiveMsg, err := icmp.ParseMessage(ipv6.ICMPTypeEchoReply.Protocol(), rb[:nBytes])
						if err != nil {
							log.Fatalf("failed to parse icmp message: %v", err)
						}

						receivedAt := time.Now()
						replyObject := ICMPReceiveReply{
							ID:         icmp6tr.id,
							Size:       nBytes,
							ReceivedAt: receivedAt,
							Peer:       peerAddr.Network() + ":" + peerAddr.String(),
							TTL:        ctrlMsg.HopLimit,
							Seq:        -1, // if can't determine, use -1
						}

						icmpBody, ok := receiveMsg.Body.(*icmp.Echo)
						if !ok {
							log.Printf("failed to parse icmp body: %+v", receiveMsg)
							continue
						}

						if icmpBody.ID != icmp6tr.id {
							// silently ignore the message that is not for us
							continue
						}

						replyObject.Seq = icmpBody.Seq
						var ty ipv6.ICMPType

						switch receiveMsg.Type {
						case ipv6.ICMPTypeTimeExceeded:
							ty = ipv6.ICMPTypeTimeExceeded
						case ipv6.ICMPTypeEchoReply:
							ty = ipv6.ICMPTypeEchoReply
						default:
							log.Printf("unknown ICMP message: %+v", receiveMsg)
							continue
						}

						replyObject.ICMPTypeV6 = &ty
						replySubCh <- replyObject
						break
					}
				}
			}
		}()

		// launch sending goroutine
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case req := <-icmp6tr.SendC:
					wm.Body.(*icmp.Echo).Seq = req.Seq
					wb, err := wm.Marshal(nil)
					if err != nil {
						log.Fatalf("failed to marshal icmp message: %v", err)
					}

					wcm.HopLimit = req.TTL
					dst := req.Dst
					if _, err := icmp6tr.ipv6PacketConn.WriteTo(wb, &wcm, &dst); err != nil {
						log.Fatalf("failed to write to connection: %v", err)
					}
				}
			}
		}()

		<-ctx.Done()
	}()

	return nil
}

func (icmp6tr *ICMP6Transceiver) GetSender() chan<- ICMPSendRequest {
	return icmp6tr.SendC
}

func (icmp6tr *ICMP6Transceiver) GetReceiver() chan<- chan ICMPReceiveReply {
	return icmp6tr.ReceiveC
}
