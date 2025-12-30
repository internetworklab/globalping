package raw

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
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

	UDPBasePort *int

	UseUDP bool
}

// add this udpbaseport with seq to get the actual udp dst port,
// e.g. seq = 1, base port = 33433, then real udp dst port = 33433 + 1 = 33434
const defaultUDPBasePort int = 33433

type ICMPSendRequest struct {
	Dst  net.IPAddr
	Seq  int
	TTL  int
	Data []byte
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
	id int

	useUDP bool

	udpBasePort int

	// User send requests to here, we retrieve the request,
	// then we translate it to the wire format.
	SendC chan ICMPSendRequest

	// ICMP replies
	ReceiveC chan chan ICMPReceiveReply
}

func NewICMP4Transceiver(config ICMP4TransceiverConfig) (*ICMP4Transceiver, error) {

	tracer := &ICMP4Transceiver{
		id:          config.ID,
		SendC:       make(chan ICMPSendRequest),
		ReceiveC:    make(chan chan ICMPReceiveReply),
		udpBasePort: defaultUDPBasePort,
		useUDP:      config.UseUDP,
	}
	if config.UDPBasePort != nil {
		tracer.udpBasePort = *config.UDPBasePort
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
			return identifier, err
		} else {
			err = fmt.Errorf("unknown origin ip protocol: %d", originIPPacket.Protocol)
			return identifier, err
		}
	} else {
		err = fmt.Errorf("unknown icmp type: %d", icmpPacket.TypeCode.Type())
		return identifier, err
	}
}

func (icmp4tr *ICMP4Transceiver) Run(ctx context.Context) <-chan error {
	errCh := make(chan error, 2)

	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		errCh <- fmt.Errorf("failed to listen on packet:icmp: %v", err)
		return errCh
	}

	// Create a raw IP connection for sending UDP packets
	rawConn, err := ipv4.NewRawConn(conn)
	if err != nil {
		errCh <- fmt.Errorf("failed to create raw connection: %v", err)
		return errCh
	}

	if err := rawConn.SetControlMessage(ipv4.FlagTTL|ipv4.FlagSrc|ipv4.FlagDst|ipv4.FlagInterface, true); err != nil {
		errCh <- fmt.Errorf("failed to set control message: %v", err)
		return errCh
	}

	// launch receiving goroutine
	go func() {
		bufSize := getMaximumMTU()
		rb := make([]byte, bufSize)

		for {
			select {
			case <-ctx.Done():
				return
			case replysSubCh := <-icmp4tr.ReceiveC:
				for {
					hdr, payload, ctrlMsg, err := rawConn.ReadFrom(rb)
					if err != nil {
						if err, ok := err.(net.Error); ok && err.Timeout() {
							continue
						}
						log.Printf("failed to read from connection: %v", err)
						break
					}

					nBytes := hdr.TotalLen

					receivedAt := time.Now()
					replyObject := ICMPReceiveReply{
						ID:         icmp4tr.id,
						Size:       nBytes,
						ReceivedAt: receivedAt,
						Peer:       hdr.Src.String(),
						TTL:        ctrlMsg.TTL,
						Seq:        -1, // if can't determine, use -1
					}
					replyObject.PeerRaw = &net.IPAddr{IP: hdr.Src}
					replyObject.PeerRawIP = &net.IPAddr{IP: hdr.Src}

					pktIdentifier, err := getIDSeqPMTUFromOriginIPPacket4(payload, icmp4tr.udpBasePort)
					if err != nil {
						log.Printf("failed to parse ip packet, skipping: %v", err)
						continue
					}

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
		defer conn.Close()
		defer rawConn.Close()

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
				var ipProtoNum int
				if icmp4tr.useUDP {
					ipProtoNum = int(layers.IPProtocolUDP)
					udpDstPort := icmp4tr.udpBasePort + req.Seq

					udpLayer := &layers.UDP{
						SrcPort: layers.UDPPort(icmp4tr.id),
						DstPort: layers.UDPPort(udpDstPort),
					}

					udpLayer.Payload = req.Data
					maxPayloadLen := 65535 - udpHeaderLen
					if len(udpLayer.Payload) > maxPayloadLen {
						udpLayer.Payload = udpLayer.Payload[:maxPayloadLen]
						log.Printf("truncated udp payload to %d bytes", maxPayloadLen)
					}

					udpTotalLen := udpHeaderLen + len(udpLayer.Payload)
					udpLayer.Length = uint16(udpTotalLen)
					if int(udpTotalLen) != int(udpLayer.Length) {
						log.Printf("udp total length mismatch, the packet will be dropped, expected: %d, got: %d", udpTotalLen, udpLayer.Length)
						continue
					}

					buf := gopacket.NewSerializeBuffer()
					opts := gopacket.SerializeOptions{}
					err = gopacket.SerializeLayers(buf, opts, udpLayer)
					if err != nil {
						log.Fatalf("failed to serialize udp layer: %v", err)
					}
					wb = buf.Bytes()
				} else {
					ipProtoNum = int(layers.IPProtocolICMPv4)
					wm := icmp.Message{
						Type: ipv4.ICMPTypeEcho,
						Code: 0,
						Body: &icmp.Echo{
							ID:   icmp4tr.id,
							Seq:  req.Seq,
							Data: req.Data,
						},
					}
					wb, err = wm.Marshal(nil)
					if err != nil {
						log.Fatalf("failed to marshal icmp message: %v", err)
					}
				}

				iph := &ipv4.Header{
					Version:  ipv4.Version,
					Len:      ipv4.HeaderLen,
					TotalLen: ipv4.HeaderLen + len(wb),
					TTL:      req.TTL,
					Flags:    ipv4.DontFragment,
					Dst:      req.Dst.IP,
					Protocol: ipProtoNum,
				}

				var cm *ipv4.ControlMessage = nil
				if err := rawConn.WriteTo(iph, wb, cm); err != nil {
					log.Fatalf("failed to write to connection: %v", err)
					continue
				}

				markAsSentBytes(ctx, iph.TotalLen)
			}
		}
	}()

	return errCh
}

func (icmp4tr *ICMP4Transceiver) GetSender() chan<- ICMPSendRequest {
	return icmp4tr.SendC
}

func (icmp4tr *ICMP4Transceiver) GetReceiver() chan<- chan ICMPReceiveReply {
	return icmp4tr.ReceiveC
}

type ICMP6TransceiverConfig struct {
	UseUDP bool
}

type ICMP6Transceiver struct {
	useUDP bool

	// User send requests to here, we retrieve the request,
	// then we translate it to the wire format.
	SendC chan ICMPSendRequest

	// ICMP replies
	ReceiveC chan chan ICMPReceiveReply
}

func NewICMP6Transceiver(config ICMP6TransceiverConfig) (*ICMP6Transceiver, error) {
	tracer := &ICMP6Transceiver{
		SendC:    make(chan ICMPSendRequest),
		ReceiveC: make(chan chan ICMPReceiveReply),
		useUDP:   config.UseUDP,
	}

	return tracer, nil
}

func (icmp6tr *ICMP6Transceiver) Run(ctx context.Context) <-chan error {

	errCh := make(chan error, 2)
	traceIdCh := make(chan int, 1)

	// launch receiving goroutine
	go func() {

		bufSize := getMaximumMTU()
		rb := make([]byte, bufSize)

		ip6Icmp := fmt.Sprintf("%d", int(layers.IPProtocolICMPv6))
		conn, err := net.ListenPacket("ip6:"+ip6Icmp, "::")
		if err != nil {
			errCh <- fmt.Errorf("failed to listen on packet:ip6-icmp: %v", err)
			return
		}
		defer conn.Close()

		packetConn := ipv6.NewPacketConn(conn)
		if err := packetConn.SetControlMessage(ipv6.FlagHopLimit|ipv6.FlagSrc|ipv6.FlagDst|ipv6.FlagInterface, true); err != nil {
			errCh <- fmt.Errorf("failed to set control message: %v", err)
			return
		}

		var f ipv6.ICMPFilter
		f.SetAll(true)
		f.Accept(ipv6.ICMPTypeTimeExceeded)
		f.Accept(ipv6.ICMPTypeEchoReply)
		f.Accept(ipv6.ICMPTypePacketTooBig)

		// when use udp for traceroute, expect to see a port-unreachable when packet reaches the end
		f.Accept(ipv6.ICMPTypeDestinationUnreachable)
		if err := packetConn.SetICMPFilter(&f); err != nil {
			log.Fatal(err)
		}

		traceId := <-traceIdCh

		for {
			select {
			case <-ctx.Done():
				return
			case replySubCh := <-icmp6tr.ReceiveC:
				for {
					nBytes, ctrlMsg, peerAddr, err := packetConn.ReadFrom(rb)
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
					ty := receiveMsg.Type.Protocol()
					cd := receiveMsg.Code

					receivedAt := time.Now()
					replyObject := ICMPReceiveReply{
						ID:         traceId,
						Size:       nBytes,
						ReceivedAt: receivedAt,
						Peer:       peerAddr.String(),
						TTL:        ctrlMsg.HopLimit,
						Seq:        -1, // if can't determine, use -1
						ICMPType:   &ty,
						ICMPCode:   &cd,
					}

					if replyObject.ID != traceId {
						// silently ignore the message that is not for us
						continue
					}

					replySubCh <- replyObject
					markAsReceivedBytes(ctx, nBytes)
					break
				}
			}
		}
	}()

	// launch sending goroutine
	go func() {
		var traceId int
		var packetConn net.PacketConn
		var err error

		var ipv6PacketConn *ipv6.PacketConn

		if icmp6tr.useUDP {
			packetConn, err = net.ListenPacket("udp", "[::]:0")
			if err != nil {
				errCh <- fmt.Errorf("failed to listen on udp: %v", err)
				return
			}

			udpAddr, ok := packetConn.LocalAddr().(*net.UDPAddr)
			if !ok {
				panic("failed to cast local address to *net.UDPAddr")
			}
			traceId = udpAddr.Port
			ipv6PacketConn = ipv6.NewPacketConn(packetConn)
		} else {
			traceId = rand.Intn(65536)
			c, err := net.ListenPacket("ip6:58", "::") // ICMP for IPv6
			if err != nil {
				errCh <- fmt.Errorf("failed to listen on packet:ip6-icmp: %v", err)
				return
			}
			ipv6PacketConn = ipv6.NewPacketConn(c)
		}

		var wcm ipv6.ControlMessage

		for {
			select {
			case <-ctx.Done():
				return
			case req, ok := <-icmp6tr.SendC:
				if !ok {
					return
				}

				var wb []byte
				if icmp6tr.useUDP {
					wm := icmp.Message{
						Type: ipv6.ICMPTypeEchoRequest, Code: 0,
						Body: &icmp.Echo{
							ID:   traceId,
							Seq:  req.Seq,
							Data: req.Data,
						},
					}
					wb, err = wm.Marshal(nil)
					if err != nil {
						log.Printf("failed to marshal icmp message: %v", err)
						continue
					}
				}

				wcm.HopLimit = req.TTL
				dst := req.Dst
				nbytes, err := ipv6PacketConn.WriteTo(wb, &wcm, &dst)
				if err != nil {
					log.Fatalf("failed to write to connection: %v", err)
				}

				markAsSentBytes(ctx, nbytes)
			}
		}
	}()

	return errCh
}

func (icmp6tr *ICMP6Transceiver) GetSender() chan<- ICMPSendRequest {
	return icmp6tr.SendC
}

func (icmp6tr *ICMP6Transceiver) GetReceiver() chan<- chan ICMPReceiveReply {
	return icmp6tr.ReceiveC
}
