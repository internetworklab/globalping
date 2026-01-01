package raw

import (
	"context"
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
	ID *int

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

	PeerRawIP *net.IPAddr `json:"-"`

	LastHop bool

	PeerRDNS []string

	ReceivedAt time.Time

	// ICMPv4 and ICMPv6 has different semantics for Type and Code,
	// so a dedicated field for indicating IP version is needed.
	INetFamily int
	ICMPType   *int
	ICMPCode   *int

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
	useUDP bool

	udpBasePort int

	SendC chan chan ICMPSendRequest

	ReceiveC chan ICMPReceiveReply
}

func NewICMP4Transceiver(config ICMP4TransceiverConfig) (*ICMP4Transceiver, error) {

	tracer := &ICMP4Transceiver{
		SendC:       make(chan chan ICMPSendRequest),
		ReceiveC:    make(chan ICMPReceiveReply),
		udpBasePort: defaultUDPBasePort,
		useUDP:      config.UseUDP,
	}
	if config.UDPBasePort != nil {
		tracer.udpBasePort = *config.UDPBasePort
	}

	return tracer, nil
}

func (icmp4tr *ICMP4Transceiver) Run(ctx context.Context) <-chan error {
	errCh := make(chan error, 2)

	traceId := rand.Intn(65536)
	if icmp4tr.useUDP {
		traceId = 1024 + rand.Intn(65536-1024)
	}

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
	// then the context is Done, the sending goroutine will exit, which also close the PacketConn by the way, once the PacketConn is closed, 
	// ReadFrom will result in error, so this receiving goroutine will return as well.
	// Event chain: context done (or cancel) -> sending goroutine exit -> PacketConn close -> ReadFrom error -> receiving goroutine return
	go func() {
		bufSize := getMaximumMTU()
		rb := make([]byte, bufSize)
		defer close(icmp4tr.ReceiveC)

		for {
			hdr, payload, ctrlMsg, err := rawConn.ReadFrom(rb)
			if err != nil {
				if err, ok := err.(net.Error); ok && err.Timeout() {
					continue
				}
				errCh <- fmt.Errorf("failed to read from connection: %v", err)
				return
			}

			nBytes := hdr.TotalLen

			receivedAt := time.Now()
			replyObject := ICMPReceiveReply{
				ID:         traceId,
				Size:       nBytes,
				ReceivedAt: receivedAt,
				Peer:       hdr.Src.String(),
				TTL:        ctrlMsg.TTL,
				Seq:        -1, // if can't determine, use -1
				INetFamily: ipv4.Version,
			}
			replyObject.PeerRawIP = &net.IPAddr{IP: hdr.Src}

			pktIdentifier, err := getIDSeqPMTUFromOriginIPPacket4(payload, icmp4tr.udpBasePort)
			if err != nil {
				log.Printf("failed to parse ip packet, skipping: %v", err)
				continue
			}

			if pktIdentifier.Id != traceId {
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

			icmp4tr.ReceiveC <- replyObject
			markAsReceivedBytes(ctx, nBytes)
			break
		}
	}()

	// launch sending goroutine
	go func() {
		defer conn.Close()
		defer rawConn.Close()
		defer close(icmp4tr.SendC)

		for {
			reqCh := make(chan ICMPSendRequest)
			select {
			case <-ctx.Done():
				return
			case icmp4tr.SendC <- reqCh:
				req, ok := <-reqCh
				if !ok {
					continue
				}

				var wb []byte = nil
				var err error = nil
				var ipProtoNum int
				if icmp4tr.useUDP {
					ipProtoNum = int(layers.IPProtocolUDP)
					udpDstPort := icmp4tr.udpBasePort + req.Seq

					udpLayer := &layers.UDP{
						SrcPort: layers.UDPPort(traceId),
						DstPort: layers.UDPPort(udpDstPort),
					}

					udpLayer.Payload = req.Data
					maxPayloadLen := 65535 - udpHeaderLen - ipv4HeaderLen
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
						errCh <- fmt.Errorf("failed to serialize udp layer: %v", err)
						return
					}
					wb = buf.Bytes()
				} else {
					ipProtoNum = int(layers.IPProtocolICMPv4)
					wm := icmp.Message{
						Type: ipv4.ICMPTypeEcho,
						Code: 0,
						Body: &icmp.Echo{
							ID:   traceId,
							Seq:  req.Seq,
							Data: req.Data,
						},
					}
					wb, err = wm.Marshal(nil)
					if err != nil {
						errCh <- fmt.Errorf("failed to marshal icmp message: %v", err)
						return
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
					errCh <- fmt.Errorf("failed to write to connection: %v", err)
					return
				}

				markAsSentBytes(ctx, iph.TotalLen)
			}
		}
	}()

	return errCh
}

func (icmp4tr *ICMP4Transceiver) GetSender() <-chan chan ICMPSendRequest {
	return icmp4tr.SendC
}

func (icmp4tr *ICMP4Transceiver) GetReceiver() <-chan ICMPReceiveReply {
	return icmp4tr.ReceiveC
}

type ICMP6TransceiverConfig struct {
	UseUDP      bool
	UDPBasePort *int
}

type ICMP6Transceiver struct {
	useUDP bool

	udpBasePort int

	SendC chan chan ICMPSendRequest

	ReceiveC chan ICMPReceiveReply
}

func NewICMP6Transceiver(config ICMP6TransceiverConfig) (*ICMP6Transceiver, error) {
	tracer := &ICMP6Transceiver{
		SendC:       make(chan chan ICMPSendRequest),
		ReceiveC:    make(chan ICMPReceiveReply),
		useUDP:      config.UseUDP,
		udpBasePort: defaultUDPBasePort,
	}
	if config.UDPBasePort != nil {
		tracer.udpBasePort = *config.UDPBasePort
	}

	return tracer, nil
}

func (icmp6tr *ICMP6Transceiver) Run(ctx context.Context) <-chan error {

	errCh := make(chan error, 2)
	traceIdCh := make(chan int, 1)

	// launch receiving goroutine
	go func() {
		defer close(icmp6tr.ReceiveC)

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
			errCh <- fmt.Errorf("failed to set icmp filter: %v", err)
			return
		}

		traceId := <-traceIdCh

		for {
			nBytes, ctrlMsg, peerAddr, err := packetConn.ReadFrom(rb)
			if err != nil {
				if err, ok := err.(net.Error); ok && err.Timeout() {
					log.Printf("timeout reading from connection, skipping")
					continue
				}
				errCh <- fmt.Errorf("failed to read from connection: %v", err)
				return
			}

			receiveMsg, err := icmp.ParseMessage(protocolNumberICMPv6, rb[:nBytes])
			if err != nil {
				log.Printf("failed to parse icmp message: %v, raw: %v", err, string(rb[:nBytes]))
				continue
			}

			ty := receiveMsg.Type.Protocol()
			cd := receiveMsg.Code

			receivedAt := time.Now()
			replyObject := ICMPReceiveReply{
				Size:       nBytes,
				ReceivedAt: receivedAt,
				Peer:       peerAddr.String(),
				TTL:        ctrlMsg.HopLimit,
				Seq:        -1, // if can't determine, use -1
				ICMPType:   &ty,
				ICMPCode:   &cd,
				INetFamily: ipv6.Version,
			}

			if peerAddr, ok := peerAddr.(*net.IPAddr); ok {
				replyObject.PeerRawIP = peerAddr
			}

			switch receiveMsg.Type {
			case ipv6.ICMPTypeEchoReply:
				echoReply, ok := receiveMsg.Body.(*icmp.Echo)
				if !ok {
					log.Printf("failed to cast echo reply body to *icmp.Echo")
					continue
				}
				replyObject.ID = echoReply.ID
				replyObject.Seq = echoReply.Seq
				replyObject.LastHop = true
				replyObject.IPProto = int(layers.IPProtocolICMPv6)
			case ipv6.ICMPTypeTimeExceeded:
				timeExceededMsg, ok := receiveMsg.Body.(*icmp.TimeExceeded)
				if !ok {
					log.Printf("failed to cast time exceeded body to *icmp.TimeExceeded")
					continue
				}
				originPktIdentifier, err := extractPacketInfoFromOriginIP6(timeExceededMsg.Data, icmp6tr.udpBasePort)
				if err != nil {
					log.Printf("failed to extract packet info from origin ip6 packet: %v", err)
					continue
				}
				replyObject.IPProto = originPktIdentifier.IPProto
				replyObject.ID = originPktIdentifier.Id
				replyObject.Seq = originPktIdentifier.Seq
			case ipv6.ICMPTypeDestinationUnreachable:
				switch receiveMsg.Code {
				case layers.ICMPv6CodePortUnreachable:
					replyObject.LastHop = true

					dstUnreachMsg, ok := receiveMsg.Body.(*icmp.DstUnreach)
					if !ok {
						log.Printf("failed to cast destination unreachable body to *icmp.DstUnreach")
						continue
					}

					originPktIdentifier, err := extractPacketInfoFromOriginIP6(dstUnreachMsg.Data, icmp6tr.udpBasePort)
					if err != nil {
						log.Printf("failed to extract packet info from origin ip6 packet: %v", err)
						continue
					}

					replyObject.IPProto = originPktIdentifier.IPProto
					replyObject.ID = originPktIdentifier.Id
					replyObject.Seq = originPktIdentifier.Seq
				default:
					log.Printf("unknown icmpv6 destination unreachable code: %v", receiveMsg.Code)
					continue
				}
			case ipv6.ICMPTypePacketTooBig:
				// usually occurs when the user is intentionally performing a PMTU trace
				packetTooBigMsg, ok := receiveMsg.Body.(*icmp.PacketTooBig)
				if !ok {
					log.Printf("failed to cast packet too big body to *icmp.PacketTooBig")
					continue
				}

				replyObject.SetMTUTo = &packetTooBigMsg.MTU

				originPktIdentifier, err := extractPacketInfoFromOriginIP6(packetTooBigMsg.Data, icmp6tr.udpBasePort)
				if err != nil {
					log.Printf("failed to extract packet info from origin ip6 packet: %v", err)
					continue
				}

				replyObject.IPProto = originPktIdentifier.IPProto
				replyObject.ID = originPktIdentifier.Id
				replyObject.Seq = originPktIdentifier.Seq
			default:
				log.Printf("unknown icmpv6 type: %v", receiveMsg.Type)
				continue
			}

			if replyObject.ID != traceId {
				// silently ignore the message that is not for us
				continue
			}

			icmp6tr.ReceiveC <- replyObject
			markAsReceivedBytes(ctx, nBytes)
			break
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
			defer packetConn.Close()

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
			defer c.Close()
			ipv6PacketConn = ipv6.NewPacketConn(c)
		}
		traceIdCh <- traceId

		var wcm ipv6.ControlMessage
		maxPayloadLen := 65535 - ipv6HeaderLen - udpHeaderLen
		for {
			reqCh := make(chan ICMPSendRequest)

			select {
			case <-ctx.Done():
				return
			case icmp6tr.SendC <- reqCh:
				req, ok := <-reqCh
				if !ok {
					continue
				}

				var dst net.Addr = &req.Dst

				var wb []byte
				if icmp6tr.useUDP {
					dst = &net.UDPAddr{
						IP:   req.Dst.IP,
						Port: icmp6tr.udpBasePort + req.Seq,
					}

					wb = req.Data
					if len(wb) > maxPayloadLen {
						wb = wb[:maxPayloadLen]
						log.Printf("truncated udp payload to %d bytes", maxPayloadLen)
					}
				} else {
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
				nbytes, err := ipv6PacketConn.WriteTo(wb, &wcm, dst)
				if err != nil {
					errCh <- fmt.Errorf("failed to write to connection: %v", err)
					return
				}

				recordSentBytes := nbytes + ipv6HeaderLen
				if icmp6tr.useUDP {
					recordSentBytes += udpHeaderLen
				}
				markAsSentBytes(ctx, recordSentBytes)
			}
		}
	}()

	return errCh
}

func (icmp6tr *ICMP6Transceiver) GetSender() <- chan chan ICMPSendRequest {
	return icmp6tr.SendC
}

func (icmp6tr *ICMP6Transceiver) GetReceiver() <-chan ICMPReceiveReply {
	return icmp6tr.ReceiveC
}
