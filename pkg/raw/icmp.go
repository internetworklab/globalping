package raw

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	pkgipinfo "example.com/rbmq-demo/pkg/ipinfo"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type ICMP4TransceiverConfig struct {
	// ICMP ID to use
	ID int
}

// add this udpbaseport with seq to get the actual udp dst port,
// e.g. seq = 1, base port = 33433, then real udp dst port = 33433 + 1 = 33434
const defaultUDPBasePort int = 33433

type ICMPSendRequest struct {
	Dst        net.IPAddr
	Seq        int
	TTL        int
	Data       []byte
	UseUDP     bool
	UDPDstPort *int
}

type ICMPReceiveReply struct {
	ID   int
	Size int
	Seq  int
	TTL  int

	// the Src of the icmp echo reply, in string
	Peer string

	PeerRaw   net.Addr    `json:"-"`
	PeerRawIP *net.IPAddr `json:"-"`

	LastHop bool

	PeerRDNS []string

	ReceivedAt time.Time

	ICMPTypeV4 *ipv4.ICMPType
	ICMPTypeV6 *ipv6.ICMPType

	ICMPType *int
	ICMPCode *int

	// IPProtocol that was sent, not reply
	IPProto int

	SetMTUTo            *int
	ShrinkICMPPayloadTo *int `json:"-"`

	// below are left for ip information provider
	PeerASN           *string
	PeerLocation      *string
	PeerISP           *string
	PeerExactLocation *pkgipinfo.ExactLocation
}

func (icmpReply *ICMPReceiveReply) ResolveIPInfo(ctx context.Context, ipinfoAdapter pkgipinfo.GeneralIPInfoAdapter) (*ICMPReceiveReply, error) {
	clonedICMPReply := new(ICMPReceiveReply)
	*clonedICMPReply = *icmpReply
	ipInfo, err := ipinfoAdapter.GetIPInfo(ctx, clonedICMPReply.Peer)
	if err != nil {
		return nil, err
	}
	if ipInfo == nil {
		return clonedICMPReply, nil
	}
	if ipInfo.ASN != "" {
		clonedICMPReply.PeerASN = &ipInfo.ASN
	}
	if ipInfo.Location != "" {
		clonedICMPReply.PeerLocation = &ipInfo.Location
	}
	if ipInfo.ISP != "" {
		clonedICMPReply.PeerISP = &ipInfo.ISP
	}
	if ipInfo.Exact != nil {
		clonedICMPReply.PeerExactLocation = ipInfo.Exact
	}
	return clonedICMPReply, nil
}

func (icmpReply *ICMPReceiveReply) ResolveRDNS(ctx context.Context, resolver *net.Resolver) (*ICMPReceiveReply, error) {
	clonedICMPReply := new(ICMPReceiveReply)
	*clonedICMPReply = *icmpReply
	ptrAnswers, err := resolver.LookupAddr(ctx, clonedICMPReply.Peer)
	if err == nil {
		clonedICMPReply.PeerRDNS = ptrAnswers
	}
	return clonedICMPReply, err
}

type ICMP4Transceiver struct {
	id             int
	packetConn     net.PacketConn
	ipv4PacketConn *ipv4.PacketConn
	ipv4RawConn    *ipv4.RawConn

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

type PacketIdentifier struct {
	Id   int
	Seq  int
	PMTU *int

	// IPProtocol that was sent, not reply
	IPProto int
	LastHop bool

	ICMPType *int
	ICMPCode *int
}

func (pktId *PacketIdentifier) String() string {
	// json bytes, jb for short.
	jb, _ := json.Marshal(pktId)
	return string(jb)
}

// with IP reply stripped, remains ICMPv4 PDU
func getIDSeqPMTUFromOriginIPPacket4(rawICMPReply []byte, baseDstPort int) (identifier *PacketIdentifier, err error) {
	identifier = new(PacketIdentifier)

	thepacket := gopacket.NewPacket(rawICMPReply, layers.LayerTypeICMPv4, gopacket.Default)
	if thepacket == nil {
		err = fmt.Errorf("failed to create/decode icmp packet")
		return identifier, err
	}

	icmpLayer := thepacket.Layer(layers.LayerTypeICMPv4)
	if icmpLayer == nil {
		err = fmt.Errorf("failed to extract icmp layer")
		return identifier, err
	}

	icmpPacket, ok := icmpLayer.(*layers.ICMPv4)
	if !ok {
		err = fmt.Errorf("failed to cast icmp layer to icmp packet")
		return identifier, err
	}

	ty := int(icmpPacket.TypeCode.Type())
	cd := int(icmpPacket.TypeCode.Code())
	identifier.ICMPType = &ty
	identifier.ICMPCode = &cd

	if icmpPacket.TypeCode.Type() == layers.ICMPv4TypeEchoReply {
		identifier.Id = int(icmpPacket.Id)
		identifier.Seq = int(icmpPacket.Seq)
		identifier.IPProto = int(layers.IPProtocolICMPv4)
		identifier.LastHop = true
		return identifier, err
	} else if icmpPacket.TypeCode.Type() == layers.ICMPv4TypeDestinationUnreachable {
		if icmpPacket.TypeCode.Code() == layers.ICMPv4CodeFragmentationNeeded && len(rawICMPReply) >= headerSizeICMP {
			pmtu := int(rawICMPReply[6])<<8 | int(rawICMPReply[7])
			identifier.PMTU = &pmtu
		}

		originPacket := gopacket.NewPacket(icmpPacket.Payload, layers.LayerTypeIPv4, gopacket.Default)
		if originPacket == nil {
			err = fmt.Errorf("failed to create/decode origin ip packet")
			return identifier, err
		}

		originIPLayer := originPacket.Layer(layers.LayerTypeIPv4)
		if originIPLayer == nil {
			err = fmt.Errorf("failed to extract origin ip layer")
			return identifier, err
		}

		originIPPacket, ok := originIPLayer.(*layers.IPv4)
		if !ok {
			err = fmt.Errorf("failed to cast origin ip layer to origin ip packet")
			return identifier, err
		}
		identifier.IPProto = int(originIPPacket.Protocol)

		if originIPPacket.Protocol == layers.IPProtocolICMPv4 {
			originICMPLayer := originPacket.Layer(layers.LayerTypeICMPv4)
			if originICMPLayer == nil {
				err = fmt.Errorf("failed to extract origin icmp layer")
				return identifier, err
			}

			originICMPPacket, ok := originICMPLayer.(*layers.ICMPv4)
			if !ok {
				err = fmt.Errorf("failed to cast origin icmp layer to origin icmp packet")
				return identifier, err
			}

			identifier.Id = int(originICMPPacket.Id)
			identifier.Seq = int(originICMPPacket.Seq)
			return identifier, err
		} else if originIPPacket.Protocol == layers.IPProtocolUDP {
			originUDPLayer := originPacket.Layer(layers.LayerTypeUDP)
			if originUDPLayer == nil {
				err = fmt.Errorf("failed to extract origin udp layer")
				return identifier, err
			}

			originUDPPacket, ok := originUDPLayer.(*layers.UDP)
			if !ok {
				err = fmt.Errorf("failed to cast origin udp layer to origin udp packet")
				return identifier, err
			}
			identifier.Id = int(originUDPPacket.SrcPort)
			identifier.Seq = int(originUDPPacket.DstPort) - baseDstPort
			identifier.LastHop = icmpPacket.TypeCode.Code() == layers.ICMPv4CodePort
			return identifier, err
		} else {
			err = fmt.Errorf("unknown origin ip protocol: %d", originIPPacket.Protocol)
			return identifier, err
		}
	} else if icmpPacket.TypeCode.Type() == layers.ICMPv4TypeTimeExceeded {
		originPacket := gopacket.NewPacket(icmpPacket.Payload, layers.LayerTypeIPv4, gopacket.Default)
		if originPacket == nil {
			err = fmt.Errorf("failed to create/decode origin ip packet")
			return identifier, err
		}

		originIPLayer := originPacket.Layer(layers.LayerTypeIPv4)
		if originIPLayer == nil {
			err = fmt.Errorf("failed to extract origin ip layer")
			return identifier, err
		}

		originIPPacket, ok := originIPLayer.(*layers.IPv4)
		if !ok {
			err = fmt.Errorf("failed to cast origin ip layer to origin ip packet")
			return identifier, err
		}
		identifier.IPProto = int(originIPPacket.Protocol)
		identifier.LastHop = false

		originICMPLayer := originPacket.Layer(layers.LayerTypeICMPv4)
		if originICMPLayer == nil {
			err = fmt.Errorf("failed to extract origin icmp layer")
			return identifier, err
		}

		originICMPPacket, ok := originICMPLayer.(*layers.ICMPv4)
		if !ok {
			err = fmt.Errorf("failed to cast origin icmp layer to origin icmp packet")
			return identifier, err
		}
		identifier.Id = int(originICMPPacket.Id)
		identifier.Seq = int(originICMPPacket.Seq)
		return identifier, err
	} else {
		err = fmt.Errorf("unknown icmp type: %d", icmpPacket.TypeCode.Type())
		return identifier, err
	}
}

func (icmp4tr *ICMP4Transceiver) Run(ctx context.Context) error {
	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("failed to listen on packet:icmp: %v", err)
	}

	// Create a raw IP connection for sending UDP packets
	rawConn, err := ipv4.NewRawConn(conn)
	if err != nil {
		return fmt.Errorf("failed to create raw connection: %v", err)
	}

	go func() {
		defer conn.Close()
		defer rawConn.Close()

		icmp4tr.packetConn = conn
		icmp4tr.ipv4PacketConn = ipv4.NewPacketConn(conn)
		if err != nil {
			log.Fatalf("failed to create raw connection: %v", err)
		}

		// Set the DF (Don't Fragment) bit to prevent routers from fragmenting packets
		// If fragmentation is needed, routers will send ICMP errors instead
		if err := setDFBit(conn); err != nil {
			log.Fatalf("failed to set DF bit: %v", err)
		}

		if err := icmp4tr.ipv4PacketConn.SetControlMessage(ipv4.FlagTTL|ipv4.FlagSrc|ipv4.FlagDst|ipv4.FlagInterface, true); err != nil {
			log.Fatal(err)
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

						receivedAt := time.Now()
						replyObject := ICMPReceiveReply{
							ID:         icmp4tr.id,
							Size:       nBytes,
							ReceivedAt: receivedAt,
							Peer:       peerAddr.String(),
							TTL:        ctrlMsg.TTL,
							Seq:        -1, // if can't determine, use -1
							PeerRaw:    peerAddr,
						}
						if ipAddr, ok := peerAddr.(*net.IPAddr); ok {
							replyObject.PeerRawIP = ipAddr
						}

						pktIdentifier, err := getIDSeqPMTUFromOriginIPPacket4(rb[:nBytes], 0)
						if err != nil {
							log.Printf("failed to parse ip packet, skipping: %v", err)
							continue
						}
						log.Printf("pktIdentifier: %s", pktIdentifier.String())

						if pktIdentifier.Id != icmp4tr.id {
							log.Printf("packet id mismatch, ignoring: %v", pktIdentifier)
							continue
						}

						replyObject.Seq = pktIdentifier.Seq
						if pktIdentifier.PMTU != nil {
							replyObject.SetMTUTo = pktIdentifier.PMTU
							shrinkTo := *pktIdentifier.PMTU - ipv4HeaderLen - headerSizeICMP
							if shrinkTo < 0 {
								shrinkTo = 0
							}
							replyObject.ShrinkICMPPayloadTo = &shrinkTo
						}
						// pure icmp packet, with ip header stripped
						replyObject.Size = nBytes
						replyObject.IPProto = pktIdentifier.IPProto
						replyObject.ICMPType = pktIdentifier.ICMPType
						replyObject.ICMPCode = pktIdentifier.ICMPCode
						replyObject.LastHop = pktIdentifier.LastHop

						replysSubCh <- replyObject
						markAsReceivedBytes(ctx, nBytes)
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
				case req, ok := <-icmp4tr.SendC:
					if !ok {
						return
					}

					var wb []byte = nil
					var err error = nil
					if req.UseUDP {
						udpDstPort := -1
						if req.UDPDstPort != nil {
							udpDstPort = *req.UDPDstPort
						} else {
							udpDstPort = defaultUDPBasePort + req.Seq
						}
						log.Printf("srcPort: %d", icmp4tr.id)
						log.Printf("udpDstPort: %d", udpDstPort)

						udpLayer := &layers.UDP{
							SrcPort: layers.UDPPort(icmp4tr.id),
							DstPort: layers.UDPPort(udpDstPort),
						}
						buf := gopacket.NewSerializeBuffer()
						opts := gopacket.SerializeOptions{}
						err = gopacket.SerializeLayers(buf, opts, udpLayer)
						if err != nil {
							log.Fatalf("failed to serialize udp layer: %v", err)
						}
						wb = buf.Bytes()

						// Append UDP payload data if present
						if req.Data != nil && len(req.Data) > 0 {
							wb = append(wb, req.Data...)
							// Update UDP length field (bytes 4-5) to include header + payload
							udpLen := uint16(len(wb))
							wb[4] = byte(udpLen >> 8)
							wb[5] = byte(udpLen & 0xff)
						}

						// Create IP header for raw UDP packet
						ipHeader := &ipv4.Header{
							Version:  4,
							Len:      ipv4.HeaderLen,
							TotalLen: ipv4.HeaderLen + len(wb),
							TTL:      req.TTL,
							Protocol: 17, // UDP IANA protocol number
							Dst:      req.Dst.IP,
							// Src will be filled by kernel based on routing
							Src: net.IPv4zero,
						}

						// Send raw UDP packet using RawConn
						err = icmp4tr.ipv4RawConn.WriteTo(ipHeader, wb, nil)
						if err != nil {
							log.Fatalf("failed to write raw UDP packet: %v", err)
						}
						markAsSentBytes(ctx, ipv4.HeaderLen+len(wb))
					} else {
						wm := icmp.Message{
							Type: ipv4.ICMPTypeEcho, Code: 0,
							Body: &icmp.Echo{
								ID:   icmp4tr.id,
								Data: nil,
							},
						}

						wm.Body.(*icmp.Echo).Seq = req.Seq
						wm.Body.(*icmp.Echo).Data = req.Data
						wb, err = wm.Marshal(nil)
						if err != nil {
							log.Fatalf("failed to marshal icmp message: %v", err)
						}

						if err := icmp4tr.ipv4PacketConn.SetTTL(req.TTL); err != nil {
							log.Fatalf("failed to set TTL to %v: %v, req: %+v", req.TTL, err, req)
						}

						dst := req.Dst
						nBytes, err := icmp4tr.ipv4PacketConn.WriteTo(wb, nil, &dst)
						if err != nil {
							log.Fatalf("failed to write to connection: %v", err)
						}
						markAsSentBytes(ctx, nBytes)
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
		f.Accept(ipv6.ICMPTypePacketTooBig)
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

						// 58 is the ICMPv6 protocol number in IPv6 header's protocol field
						receiveMsg, err := icmp.ParseMessage(protocolNumberICMPv6, rb[:nBytes])
						if err != nil {
							log.Fatalf("failed to parse icmp message: %v", err)
						}

						receivedAt := time.Now()
						replyObject := ICMPReceiveReply{
							ID:         icmp6tr.id,
							Size:       nBytes,
							ReceivedAt: receivedAt,
							Peer:       peerAddr.String(),
							TTL:        ctrlMsg.HopLimit,
							Seq:        -1, // if can't determine, use -1
						}

						var ty ipv6.ICMPType

						switch receiveMsg.Type {
						case ipv6.ICMPTypePacketTooBig:
							ty = ipv6.ICMPTypePacketTooBig
							packetTooBigBody, ok := receiveMsg.Body.(*icmp.PacketTooBig)
							if !ok {
								log.Printf("Invalid ICMP Packet Too Big body: %+v", receiveMsg)
								continue
							}

							// Extract MTU from PacketTooBig message
							mtu := packetTooBigBody.MTU
							replyObject.SetMTUTo = &mtu
							shrinkTo := mtu - ipv6HeaderLen - headerSizeICMP
							if shrinkTo < 0 {
								shrinkTo = 0
							}
							replyObject.ShrinkICMPPayloadTo = &shrinkTo

							// Extract original ICMP message from PacketTooBig body
							if len(packetTooBigBody.Data) < ipv6HeaderLen+headerSizeICMP {
								log.Printf("Invalid ICMP Packet Too Big body: %+v", receiveMsg)
								continue
							}

							// Skip IPv6 header (fixed 40 bytes)
							originalICMPData := packetTooBigBody.Data[ipv6HeaderLen:]
							originalICMPMsg, err := icmp.ParseMessage(protocolNumberICMPv6, originalICMPData)
							if err != nil {
								log.Printf("Invalid ICMP Packet Too Big message: %+v error: %+v", receiveMsg, err)
								continue
							}

							echoBody, ok := originalICMPMsg.Body.(*icmp.Echo)
							if !ok {
								log.Printf("Invalid ICMP Packet Too Big message: %+v", receiveMsg)
								continue
							}

							replyObject.Seq = echoBody.Seq
							replyObject.ID = echoBody.ID
						case ipv6.ICMPTypeTimeExceeded:
							ty = ipv6.ICMPTypeTimeExceeded

							// Extract original ICMP message from TimeExceeded body
							timeExceededBody, ok := receiveMsg.Body.(*icmp.TimeExceeded)

							// Skip IPv6 header (fixed 40 bytes)

							if !ok || len(timeExceededBody.Data) < ipv6HeaderLen+headerSizeICMP {
								log.Printf("Invalid ICMP Time-Exceeded body: %+v", receiveMsg)
								continue
							}

							// Parse the original ICMP message (protocol 58 for ICMPv6)
							originalICMPData := timeExceededBody.Data[ipv6HeaderLen:]
							originalICMPMsg, err := icmp.ParseMessage(protocolNumberICMPv6, originalICMPData)
							if err != nil {
								log.Printf("Invalid ICMP Time-Exceeded message: %+v error: %+v", receiveMsg, err)
								continue
							}

							echoBody, ok := originalICMPMsg.Body.(*icmp.Echo)
							if !ok {
								log.Printf("Invalid ICMP Time-Exceeded message: %+v", receiveMsg)
								continue
							}

							replyObject.Seq = echoBody.Seq
							replyObject.ID = echoBody.ID

						case ipv6.ICMPTypeEchoReply:
							ty = ipv6.ICMPTypeEchoReply
							icmpBody, ok := receiveMsg.Body.(*icmp.Echo)
							if !ok {
								log.Printf("failed to parse icmp body: %+v", receiveMsg)
								continue
							}
							replyObject.Seq = icmpBody.Seq
							replyObject.ID = icmpBody.ID
						default:
							log.Printf("unknown ICMP message: %+v", receiveMsg)
							continue
						}

						if replyObject.ID != icmp6tr.id {
							// silently ignore the message that is not for us
							continue
						}

						replyObject.ICMPTypeV6 = &ty
						replySubCh <- replyObject
						markAsReceivedBytes(ctx, nBytes)
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
				case req, ok := <-icmp6tr.SendC:
					if !ok {
						return
					}

					wm.Body.(*icmp.Echo).Seq = req.Seq
					wm.Body.(*icmp.Echo).Data = req.Data
					wb, err := wm.Marshal(nil)
					if err != nil {
						log.Fatalf("failed to marshal icmp message: %v", err)
					}

					wcm.HopLimit = req.TTL
					dst := req.Dst
					nbytes, err := icmp6tr.ipv6PacketConn.WriteTo(wb, &wcm, &dst)
					if err != nil {
						log.Fatalf("failed to write to connection: %v", err)
					}
					markAsSentBytes(ctx, nbytes)
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
