package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/vishvananda/netlink"
	"golang.org/x/net/ipv4"
)

type PacketInfo struct {
	Hdr     *ipv4.Header
	Payload []byte
	CtrlMsg *ipv4.ControlMessage
	TCP     *layers.TCP
}

func getPackets(rawConn *ipv4.RawConn) <-chan *PacketInfo {
	rbCh := make(chan *PacketInfo)
	rb := make([]byte, pkgutils.GetMaximumMTU())

	go func() {
		defer close(rbCh)

		for {
			hdr, payload, ctrlMsg, err := rawConn.ReadFrom(rb)
			if err != nil {
				log.Printf("failed to read from raw connection: %v", err)
				return
			}
			pktInfo := new(PacketInfo)
			pktInfo.Hdr = hdr
			pktInfo.Payload = make([]byte, hdr.TotalLen)
			copy(pktInfo.Payload, payload)
			pktInfo.CtrlMsg = ctrlMsg
			rbCh <- pktInfo
		}

	}()
	return rbCh
}

func filterPackets(rbCh <-chan *PacketInfo) <-chan *PacketInfo {
	filteredCh := make(chan *PacketInfo)
	go func() {
		defer close(filteredCh)
		for pktInfo := range rbCh {
			hdr := pktInfo.Hdr
			if hdr == nil {
				continue
			}
			if hdr.Protocol != int(layers.IPProtocolTCP) {
				continue
			}

			packet := gopacket.NewPacket(pktInfo.Payload, layers.LayerTypeTCP, gopacket.Default)
			if packet == nil {
				continue
			}

			tcpLayer := packet.Layer(layers.LayerTypeTCP)
			if tcpLayer == nil {
				continue
			}

			tcp, ok := tcpLayer.(*layers.TCP)
			if !ok {
				continue
			}

			if (!tcp.SYN) || (!tcp.ACK) {
				continue
			}

			newPacket := new(PacketInfo)
			*newPacket = *pktInfo
			newPacket.TCP = tcp
			filteredCh <- newPacket
		}
	}()

	return filteredCh
}

type Tracker struct{}

func NewTracker() *Tracker {
	return &Tracker{}
}

func (tk *Tracker) Run(ctx context.Context) {
	// todo
}

func (tk *Tracker) MarkSent(sentReceipt *TCPSYNSentReceipt) {
	// todo
}

func (tk *Tracker) MarkReceived(receivedPkt *PacketInfo) {
	// todo
}

type TCPSYNSentReceipt struct {
	SrcIP       net.IP
	SrcPort     int
	Request     *TCPSYNRequest
	SentAt      time.Time
	TimeoutC    chan time.Time
	ReceivedAt  time.Time
	ReceivedPkt *PacketInfo
	ReceivedC   chan *PacketInfo
}

type TCPSYNSender struct {
}

const defaultTTL int = 64

type TCPSYNRequest struct {
	DstIP   net.IP
	DstPort int
	Timeout time.Duration
	TTL     *int
}

func getSrcIP(dstIP net.IP) (net.IP, error) {
	handle, err := netlink.NewHandle()
	if err != nil {
		return nil, fmt.Errorf("failed to create netlink handle: %v", err)
	}
	defer handle.Close()

	routes, err := handle.RouteGet(dstIP)
	if err != nil {
		return nil, fmt.Errorf("failed to get routes for %s: %v", dstIP.String(), err)
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes found for %s", dstIP.String())
	}

	return routes[0].Src, nil
}

func (sender *TCPSYNSender) Send(rawConn *ipv4.RawConn, request *TCPSYNRequest) (*TCPSYNSentReceipt, error) {
	receipt := new(TCPSYNSentReceipt)
	receipt.TimeoutC = make(chan time.Time)
	dstIP := request.DstIP
	srcIP, err := getSrcIP(dstIP)
	if err != nil {
		return nil, fmt.Errorf("failed to determine src IP for %s: %v", dstIP.String(), err)
	}

	tcpListener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		log.Fatalf("failed to listen on tcp: %v", err)
	}
	localPort := tcpListener.Addr().(*net.TCPAddr).Port

	var ttl int = defaultTTL
	if request.TTL != nil {
		ttl = *request.TTL
	}
	ipProto := layers.IPProtocolTCP
	var flags layers.IPv4Flag
	flags = flags | layers.IPv4DontFragment

	hdrLayer := &layers.IPv4{
		SrcIP:    srcIP,
		DstIP:    dstIP,
		TTL:      uint8(ttl),
		Protocol: ipProto,
		Flags:    flags,
	}

	// length of tcp header, in unit of words (4 bytes)
	// so, 5 words means 5 word * 4 bytes/word = 20 bytes
	tcpHdrLenNWords := 5
	tcpLayer := &layers.TCP{
		SrcPort:    layers.TCPPort(localPort),
		DstPort:    layers.TCPPort(request.DstPort),
		Seq:        1000,
		Ack:        0,
		SYN:        true,
		DataOffset: uint8(tcpHdrLenNWords),
	}
	tcpLayer.SetNetworkLayerForChecksum(hdrLayer)
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		ComputeChecksums: true,
	}

	if err := gopacket.SerializeLayers(buf, opts, tcpLayer); err != nil {
		return nil, fmt.Errorf("failed to serialize tcp layer: %v", err)
	}

	wb := buf.Bytes()
	hdr := &ipv4.Header{
		Version:  ipv4.Version,
		Len:      ipv4.HeaderLen,
		TotalLen: ipv4.HeaderLen + len(wb),
		TTL:      ttl,
		Protocol: int(ipProto),
		Dst:      dstIP,
		Flags:    ipv4.HeaderFlags(flags),
	}
	err = rawConn.WriteTo(hdr, wb, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to write to raw connection: %v", err)
	}

	go func() {
		defer tcpListener.Close()

		receipt.SentAt = time.Now()
		timer := time.NewTimer(request.Timeout)
		defer timer.Stop()

		select {
		case time := <-timer.C:
			receipt.TimeoutC <- time
		case pktInfo, ok := <-receipt.ReceivedC:
			if !ok {
				return
			}
			receipt.ReceivedAt = time.Now()
			receipt.ReceivedPkt = pktInfo
		}
	}()
	return receipt, nil
}

func getRawIPv4Conn(ctx context.Context) (*ipv4.RawConn, error) {
	listenConfig := net.ListenConfig{}

	ipProtoTCP := fmt.Sprintf("%d", int(layers.IPProtocolTCP))
	ln, err := listenConfig.ListenPacket(ctx, "ip4:"+ipProtoTCP, "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to create raw tcp/ip socket: %v", err)
	}

	defer ln.Close()

	log.Printf("listening on %s", ln.LocalAddr().String())

	rawConn, err := ipv4.NewRawConn(ln)
	if err != nil {
		return nil, fmt.Errorf("failed to create raw connection: %v", err)
	}
	return rawConn, nil
}

func main() {

	dstIP := net.ParseIP("172.17.0.7")
	dstPort := 80

	ctx := context.Background()

	rawConn, err := getRawIPv4Conn(ctx)
	if err != nil {
		log.Fatalf("failed to get raw ipv4 connection: %v", err)
	}
	log.Printf("raw connection created")

	rbCh := getPackets(rawConn)
	filteredCh := filterPackets(rbCh)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sender := &TCPSYNSender{}
	tracker := NewTracker()
	tracker.Run(ctx)

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case pktInfo, ok := <-filteredCh:
				if !ok {
					log.Printf("filteredCh is closed")
				}
				tcp := pktInfo.TCP
				if tcp == nil {
					continue
				}
				hdr := pktInfo.Hdr
				if hdr == nil {
					continue
				}
				tracker.MarkReceived(pktInfo)
			}
		}
	}(ctx)

	receipt, err := sender.Send(rawConn, &TCPSYNRequest{
		DstIP:   dstIP,
		DstPort: dstPort,
		Timeout: 3 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to send tcp syn: %v", err)
	}

	tracker.MarkSent(receipt)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	log.Printf("received signal: %s", sig.String())
}
