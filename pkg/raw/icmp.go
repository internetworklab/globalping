package raw

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"syscall"
	"time"

	pkgipinfo "example.com/rbmq-demo/pkg/ipinfo"
	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type ICMPTransceiverHook func(ctx context.Context, request *ICMPSendRequest, reply *ICMPReceiveReply, peer string, nBytes int) error

type ICMP4TransceiverConfig struct {
	// ICMP ID to use
	ID *int

	UDPBasePort *int

	UseUDP bool

	OnSent     ICMPTransceiverHook
	OnReceived ICMPTransceiverHook
}

// add this udpbaseport with seq to get the actual udp dst port,
// e.g. seq = 1, base port = 33433, then real udp dst port = 33433 + 1 = 33434
const defaultUDPBasePort int = 33433

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
	clonedICMPReply.PeerIPInfo = ipInfo
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

	closed         bool
	closeCh        chan interface{}
	closeProtector sync.Mutex

	onSent     ICMPTransceiverHook
	onReceived ICMPTransceiverHook
}

func NewICMP4Transceiver(config ICMP4TransceiverConfig) (*ICMP4Transceiver, error) {

	tracer := &ICMP4Transceiver{
		SendC:          make(chan chan ICMPSendRequest),
		ReceiveC:       make(chan ICMPReceiveReply),
		udpBasePort:    defaultUDPBasePort,
		useUDP:         config.UseUDP,
		closeCh:        make(chan interface{}),
		closeProtector: sync.Mutex{},
		onSent:         config.OnSent,
		onReceived:     config.OnReceived,
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

	listenConfig := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MTU_DISCOVER, syscall.IP_PMTUDISC_PROBE); err != nil {
					panic(fmt.Errorf("failed to set IP_MTU_DISCOVER to IP_PMTUDISC_PROBE: %v", err))
				}
			})
		},
	}

	conn, err := listenConfig.ListenPacket(context.Background(), "ip4:icmp", "0.0.0.0")
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
		rb := make([]byte, pkgutils.GetMaximumMTU())
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
				shrinkTo := *pktIdentifier.PMTU - ipv4.HeaderLen - headerSizeICMP
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
			if icmp4tr.onReceived != nil {
				if err := icmp4tr.onReceived(ctx, nil, &replyObject, replyObject.Peer, nBytes); err != nil {
					errCh <- fmt.Errorf("failed to call onReceived callback: %v", err)
					return
				}
			}
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
			case <-icmp4tr.closeCh:
				icmp4tr.closed = true
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

					payloadData := req.Data
					maxPayloadLen := GetMaxPayloadLen(ipv4.Version, int(layers.IPProtocolUDP), req.PMTU, req.NexthopMTU)
					if len(payloadData) > maxPayloadLen {
						payloadData = payloadData[:maxPayloadLen]
					}

					udpTotalLen := udpHeaderLen + len(payloadData)
					udpLayer.Length = uint16(udpTotalLen)
					if int(udpTotalLen) != int(udpLayer.Length) {
						log.Printf("udp total length mismatch, the packet will be dropped, expected: %d, got: %d", udpTotalLen, udpLayer.Length)
						continue
					}

					buf := gopacket.NewSerializeBuffer()
					opts := gopacket.SerializeOptions{}
					payloadLayer := gopacket.Payload(payloadData)
					err = payloadLayer.SerializeTo(buf, opts)
					if err != nil {
						errCh <- fmt.Errorf("failed to serialize payload layer of udp: %v", err)
						return
					}
					err = udpLayer.SerializeTo(buf, opts)
					if err != nil {
						errCh <- fmt.Errorf("failed to serialize udp layer: %v", err)
						return
					}
					wb = buf.Bytes()
				} else {
					ipProtoNum = int(layers.IPProtocolICMPv4)
					icmpEcho := &icmp.Echo{
						ID:   traceId,
						Seq:  req.Seq,
						Data: req.Data,
					}
					maxPayloadLen := GetMaxPayloadLen(ipv4.Version, int(layers.IPProtocolICMPv4), req.PMTU, req.NexthopMTU)
					if len(icmpEcho.Data) > maxPayloadLen {
						icmpEcho.Data = icmpEcho.Data[:maxPayloadLen]
					}
					wm := icmp.Message{
						Type: ipv4.ICMPTypeEcho,
						Code: 0,
						Body: icmpEcho,
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
				if err := rawConn.WriteTo(iph, wb, cm); err != nil && isFatalErr(err) {
					errCh <- fmt.Errorf("failed to write to connection: %v", err)
					return
				}

				if icmp4tr.onSent != nil {
					if err := icmp4tr.onSent(ctx, &req, nil, req.Dst.String(), iph.TotalLen); err != nil {
						errCh <- fmt.Errorf("failed to call onSent callback: %v", err)
						return
					}
				}
			}
		}
	}()

	return errCh
}

func (icmp4tr *ICMP4Transceiver) Close() error {
	icmp4tr.closeProtector.Lock()
	defer icmp4tr.closeProtector.Unlock()
	if icmp4tr.closed {
		return fmt.Errorf("icmp4 transceiver is already closed")
	}

	close(icmp4tr.closeCh)
	return nil
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
	OnSent      ICMPTransceiverHook
	OnReceived  ICMPTransceiverHook
}

type ICMP6Transceiver struct {
	useUDP bool

	udpBasePort int

	SendC chan chan ICMPSendRequest

	ReceiveC chan ICMPReceiveReply

	closed         bool
	closeProtector sync.Mutex
	closeCh        chan interface{}

	onSent     ICMPTransceiverHook
	onReceived ICMPTransceiverHook
}

func NewICMP6Transceiver(config ICMP6TransceiverConfig) (*ICMP6Transceiver, error) {
	tracer := &ICMP6Transceiver{
		SendC:          make(chan chan ICMPSendRequest),
		ReceiveC:       make(chan ICMPReceiveReply),
		useUDP:         config.UseUDP,
		udpBasePort:    defaultUDPBasePort,
		closeCh:        make(chan interface{}),
		closeProtector: sync.Mutex{},
		onSent:         config.OnSent,
		onReceived:     config.OnReceived,
	}
	if config.UDPBasePort != nil {
		tracer.udpBasePort = *config.UDPBasePort
	}

	return tracer, nil
}

func (icmp6tr *ICMP6Transceiver) getSenderAndTraceId() (ipv6PacketConn *ipv6.PacketConn, traceId int, err error) {
	// var ipv6PacketConn *ipv6.PacketConn
	// var traceId int
	// var packetConn net.PacketConn
	// var err error

	listenConfig := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_MTU_DISCOVER, syscall.IPV6_PMTUDISC_PROBE); err != nil {
					panic(fmt.Errorf("failed to set IPV6_MTU_DISCOVER to IPV6_PMTUDISC_PROBE: %v", err))
				}

				// see rfc3542, section 11.2 "Sending without Fragmentation"
				const IPV6_DONTFRAG int = 62
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, IPV6_DONTFRAG, 1); err != nil {
					panic(fmt.Errorf("failed to set IPV6_DONTFRAG (62): %v", err))
				}
			})
		},
	}

	if icmp6tr.useUDP {
		var packetConn net.PacketConn
		packetConn, err = listenConfig.ListenPacket(context.Background(), "udp", "[::]:0")
		if err != nil {
			err = fmt.Errorf("failed to listen on udp: %v", err)
			return
		}

		udpAddr, ok := packetConn.LocalAddr().(*net.UDPAddr)
		if !ok {
			panic("failed to cast local address to *net.UDPAddr")
		}
		traceId = udpAddr.Port
		ipv6PacketConn = ipv6.NewPacketConn(packetConn)
	} else {
		var packetConn net.PacketConn
		traceId = rand.Intn(65536)

		packetConn, err = listenConfig.ListenPacket(context.Background(), "ip6:58", "::") // ICMP for IPv6
		if err != nil {
			err = fmt.Errorf("failed to listen on packet:ip6-icmp: %v", err)
			return
		}
		ipv6PacketConn = ipv6.NewPacketConn(packetConn)
	}

	return
}

func (icmp6tr *ICMP6Transceiver) getPacketListener() (*ipv6.PacketConn, error) {
	ip6Icmp := fmt.Sprintf("%d", int(layers.IPProtocolICMPv6))
	conn, err := net.ListenPacket("ip6:"+ip6Icmp, "::")
	if err != nil {
		return nil, fmt.Errorf("failed to listen on packet:ip6-icmp: %v", err)
	}

	packetConn := ipv6.NewPacketConn(conn)
	if err := packetConn.SetControlMessage(ipv6.FlagHopLimit|ipv6.FlagSrc|ipv6.FlagDst|ipv6.FlagInterface|ipv6.FlagPathMTU, true); err != nil {
		return nil, fmt.Errorf("failed to set control message: %v", err)
	}

	var f ipv6.ICMPFilter
	f.SetAll(true)
	f.Accept(ipv6.ICMPTypeTimeExceeded)
	f.Accept(ipv6.ICMPTypeEchoReply)
	f.Accept(ipv6.ICMPTypePacketTooBig)

	// when use udp for traceroute, expect to see a port-unreachable when packet reaches the end
	f.Accept(ipv6.ICMPTypeDestinationUnreachable)
	if err := packetConn.SetICMPFilter(&f); err != nil {
		return nil, fmt.Errorf("failed to set icmp filter: %v", err)
	}

	return packetConn, nil
}

func (icmp6tr *ICMP6Transceiver) getPacket6(rxIPv6PacketConn *ipv6.PacketConn, traceId int) (*ICMPReceiveReply, error) {
	rb := make([]byte, pkgutils.GetMaximumMTU())
	nBytes, ctrlMsg, peerAddr, err := rxIPv6PacketConn.ReadFrom(rb)

	if err != nil {
		if err, ok := err.(net.Error); ok && err.Timeout() {
			log.Printf("timeout reading from connection, skipping")
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read from connection: %v", err)
	}

	receiveMsg, err := icmp.ParseMessage(int(layers.IPProtocolICMPv6), rb[:nBytes])
	if err != nil {
		log.Printf("failed to parse icmp message: %v, raw: %v", err, string(rb[:nBytes]))
		return nil, nil
	}

	ty := receiveMsg.Type.Protocol()
	cd := receiveMsg.Code

	receivedAt := time.Now()
	replyObject := ICMPReceiveReply{
		Size:       nBytes + ipv6.HeaderLen,
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
			return nil, nil
		}
		replyObject.ID = echoReply.ID
		replyObject.Seq = echoReply.Seq
		replyObject.LastHop = true
		replyObject.IPProto = int(layers.IPProtocolICMPv6)
	case ipv6.ICMPTypeTimeExceeded:
		timeExceededMsg, ok := receiveMsg.Body.(*icmp.TimeExceeded)
		if !ok {
			log.Printf("failed to cast time exceeded body to *icmp.TimeExceeded")
			return nil, nil
		}
		originPktIdentifier, err := ExtractPacketInfoFromOriginIP6(timeExceededMsg.Data, icmp6tr.udpBasePort)
		if err != nil {
			log.Printf("failed to extract packet info from origin ip6 packet: %v", err)
			return nil, nil
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
				return nil, nil
			}

			originPktIdentifier, err := ExtractPacketInfoFromOriginIP6(dstUnreachMsg.Data, icmp6tr.udpBasePort)
			if err != nil {
				log.Printf("failed to extract packet info from origin ip6 packet: %v", err)
				return nil, nil
			}

			replyObject.IPProto = originPktIdentifier.IPProto
			replyObject.ID = originPktIdentifier.Id
			replyObject.Seq = originPktIdentifier.Seq
		default:
			log.Printf("unknown icmpv6 destination unreachable code: %v", receiveMsg.Code)
			return nil, nil
		}
	case ipv6.ICMPTypePacketTooBig:
		// usually occurs when the user is intentionally performing a PMTU trace
		packetTooBigMsg, ok := receiveMsg.Body.(*icmp.PacketTooBig)
		if !ok {
			log.Printf("failed to cast packet too big body to *icmp.PacketTooBig")
			return nil, nil
		}

		replyObject.SetMTUTo = &packetTooBigMsg.MTU

		originPktIdentifier, err := ExtractPacketInfoFromOriginIP6(packetTooBigMsg.Data, icmp6tr.udpBasePort)
		if err != nil {
			log.Printf("failed to extract packet info from origin ip6 packet: %v", err)
			return nil, nil
		}

		replyObject.IPProto = originPktIdentifier.IPProto
		replyObject.ID = originPktIdentifier.Id
		replyObject.Seq = originPktIdentifier.Seq
	default:
		log.Printf("unknown icmpv6 type: %v", receiveMsg.Type)
		return nil, nil
	}

	if replyObject.ID != traceId {
		// silently ignore the message that is not for us
		return nil, nil
	}

	return &replyObject, nil
}

func (icmp6tr *ICMP6Transceiver) sendPacket6(ctx context.Context, req ICMPSendRequest, txIPv6PacketConn *ipv6.PacketConn, traceId int) error {
	var dst net.Addr = &req.Dst
	var wcm ipv6.ControlMessage
	var err error
	var wb []byte

	if icmp6tr.useUDP {
		dst = &net.UDPAddr{
			IP:   req.Dst.IP,
			Port: icmp6tr.udpBasePort + req.Seq,
		}

		maxPayloadLen := GetMaxPayloadLen(ipv6.Version, int(layers.IPProtocolUDP), req.PMTU, req.NexthopMTU)
		wb = req.Data
		if len(wb) > maxPayloadLen {
			wb = wb[:maxPayloadLen]
		}
	} else {
		icmpEcho := &icmp.Echo{
			ID:   traceId,
			Seq:  req.Seq,
			Data: req.Data,
		}
		maxPayloadLen := GetMaxPayloadLen(ipv6.Version, int(layers.IPProtocolICMPv6), req.PMTU, req.NexthopMTU)
		if len(icmpEcho.Data) > maxPayloadLen {
			icmpEcho.Data = icmpEcho.Data[:maxPayloadLen]
		}
		wm := icmp.Message{
			Type: ipv6.ICMPTypeEchoRequest, Code: 0,
			Body: icmpEcho,
		}
		wb, err = wm.Marshal(nil)
		if err != nil {
			return fmt.Errorf("failed to marshal icmp message: %v", err)
		}
	}

	wcm.HopLimit = req.TTL
	nbytes, err := txIPv6PacketConn.WriteTo(wb, &wcm, dst)
	if err != nil {
		log.Printf("failed to write to connection, wcm: %v, dst: %v, error: %v", wcm, dst, err)
	}

	if err != nil && isFatalErr(err) {
		return fmt.Errorf("failed to write to connection: %v", err)
	}

	if icmp6tr.onSent != nil {
		recordSentBytes := nbytes + ipv6.HeaderLen
		if icmp6tr.useUDP {
			recordSentBytes += udpHeaderLen
		}
		if err := icmp6tr.onSent(ctx, &req, nil, req.Dst.String(), recordSentBytes); err != nil {
			return fmt.Errorf("failed to call onSent callback: %v", err)
		}
	}

	return nil
}

func (icmp6tr *ICMP6Transceiver) GetIO(ctx context.Context) (chan<- ICMPSendRequest, <-chan ICMPReceiveReply, <-chan error) {

	errCh := make(chan error, 2)
	sendC := make(chan ICMPSendRequest)
	receiveC := make(chan ICMPReceiveReply)

	go func(ctx context.Context) {
		txIPv6PacketConn, traceId, err := icmp6tr.getSenderAndTraceId()
		if err != nil {
			errCh <- fmt.Errorf("failed to obtain sender PacketConn and ipv6PacketConn: %v", err)
			return
		}
		defer txIPv6PacketConn.Close()

		rxIPv6PacketConn, err := icmp6tr.getPacketListener()
		if err != nil {
			errCh <- fmt.Errorf("failed to obtain packet listener: %v", err)
			return
		}
		defer rxIPv6PacketConn.Close()

		// launch receiving goroutine
		go func() {
			defer close(receiveC)

			for {
				replyObject, err := icmp6tr.getPacket6(rxIPv6PacketConn, traceId)
				if err != nil {
					// getPacket6 is guaranteed to return non-nil error only when the error is non-recoverable.
					errCh <- fmt.Errorf("failed to get packet6: %v", err)
					return
				}

				if replyObject != nil {
					receiveC <- *replyObject
					if icmp6tr.onReceived != nil {
						if err := icmp6tr.onReceived(ctx, nil, replyObject, replyObject.Peer, replyObject.Size); err != nil {
							errCh <- fmt.Errorf("failed to call onReceived callback: %v", err)
							return
						}
					}
				}
			}
		}()

		// launch sending goroutine
		for {
			select {
			case <-ctx.Done():
				return
			case req, ok := <-sendC:
				if !ok {
					return
				}

				if err := icmp6tr.sendPacket6(ctx, req, txIPv6PacketConn, traceId); err != nil {
					errCh <- fmt.Errorf("failed to send packet: %v", err)
					return
				}
			}
		}
	}(ctx)

	return sendC, receiveC, errCh
}

func (icmp6tr *ICMP6Transceiver) Run(ctx context.Context) <-chan error {

	errCh := make(chan error, 2)

	txIPv6PacketConn, traceId, err := icmp6tr.getSenderAndTraceId()
	if err != nil {
		errCh <- fmt.Errorf("failed to obtain sender PacketConn and ipv6PacketConn: %v", err)
		return errCh
	}

	rxIPv6PacketConn, err := icmp6tr.getPacketListener()
	if err != nil {
		errCh <- fmt.Errorf("failed to obtain packet listener: %v", err)
		return errCh
	}

	// launch receiving goroutine
	go func() {
		defer close(icmp6tr.ReceiveC)
		defer rxIPv6PacketConn.Close()

		for {
			replyObject, err := icmp6tr.getPacket6(rxIPv6PacketConn, traceId)
			if err != nil {
				// getPacket6 is guaranteed to return non-nil error only when the error is non-recoverable.
				errCh <- fmt.Errorf("failed to get packet6: %v", err)
				return
			}

			if replyObject != nil {
				icmp6tr.ReceiveC <- *replyObject
				if icmp6tr.onReceived != nil {
					if err := icmp6tr.onReceived(ctx, nil, replyObject, replyObject.Peer, replyObject.Size); err != nil {
						errCh <- fmt.Errorf("failed to call onReceived callback: %v", err)
						return
					}
				}
			}
		}
	}()

	// launch sending goroutine
	go func() {

		defer txIPv6PacketConn.Close()

		for {
			reqCh := make(chan ICMPSendRequest)

			select {
			case <-ctx.Done():
				return
			case <-icmp6tr.closeCh:
				icmp6tr.closed = true
				return
			case icmp6tr.SendC <- reqCh:
				req, ok := <-reqCh
				if !ok {
					continue
				}

				if err := icmp6tr.sendPacket6(ctx, req, txIPv6PacketConn, traceId); err != nil {
					errCh <- fmt.Errorf("failed to send packet: %v", err)
					return
				}
			}
		}
	}()

	return errCh
}

func (icmp6tr *ICMP6Transceiver) GetSender() <-chan chan ICMPSendRequest {
	return icmp6tr.SendC
}

func (icmp6tr *ICMP6Transceiver) GetReceiver() <-chan ICMPReceiveReply {
	return icmp6tr.ReceiveC
}

func (icmp6tr *ICMP6Transceiver) Close() error {
	icmp6tr.closeProtector.Lock()
	defer icmp6tr.closeProtector.Unlock()
	if icmp6tr.closed {
		return fmt.Errorf("icmp6 transceiver is already closed")
	}

	close(icmp6tr.closeCh)
	return nil
}
