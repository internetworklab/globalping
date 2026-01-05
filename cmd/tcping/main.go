package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/google/btree"
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

func (pktInfo *PacketInfo) String() string {
	return fmt.Sprintf("%s:%d -> %s:%d", pktInfo.Hdr.Src, pktInfo.TCP.SrcPort, pktInfo.Hdr.Dst, pktInfo.TCP.DstPort)
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
			if hdr.Version != ipv4.Version {
				continue
			}
			if hdr.Protocol != int(layers.IPProtocolTCP) {
				continue
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

type TrackEntry struct {
	Key   []byte
	Value *TCPSYNSentReceipt
}

func (tent *TrackEntry) Less(other btree.Item) bool {
	otherEntry, ok := other.(*TrackEntry)
	if !ok {
		panic("other is not a TrackEntry")
	}

	if len(tent.Key) != len(otherEntry.Key) {
		panic("keys are not of the same length")
	}

	return bytes.Compare(tent.Key, otherEntry.Key) < 0
}

type ServiceRequest struct {
	Fn func(ctx context.Context) error
	// Result channel is provided by the caller
	Result chan error
}

type TrackerEventType string

const (
	TrackerEVTimeout  TrackerEventType = "timeout"
	TrackerEVReceived TrackerEventType = "received"
)

type TrackerEvent struct {
	Type  TrackerEventType
	Entry *TrackEntry
}

type Tracker struct {
	serviceChan chan chan ServiceRequest
	EventC      chan TrackerEvent
	store       *btree.BTree
}

type TrackerConfig struct {
	EVBufferSize *int
}

func NewTracker(config *TrackerConfig) *Tracker {
	tracker := new(Tracker)
	tracker.serviceChan = make(chan chan ServiceRequest)
	tracker.store = btree.New(2)
	tracker.EventC = make(chan TrackerEvent)
	if config != nil {
		if config.EVBufferSize != nil {
			tracker.EventC = make(chan TrackerEvent, *config.EVBufferSize)
		}
	}
	return tracker
}

func (tk *Tracker) Run(ctx context.Context) {
	go func() {
		defer close(tk.serviceChan)
		defer close(tk.EventC)

		for {
			requestCh := make(chan ServiceRequest)
			select {
			case <-ctx.Done():
				return
			case tk.serviceChan <- requestCh:
				request := <-requestCh
				request.Result <- request.Fn(ctx)
			}
		}
	}()
}

func encodePort(port int) []byte {
	if port < 0 || port > 65535 {
		panic("port is out of range")
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	return portBytes
}

func buildKey(srcIP net.IP, srcPort int, dstIP net.IP, dstPort int) []byte {
	key := make([]byte, 0)
	key = append(key, srcIP...)
	key = append(key, encodePort(srcPort)...)
	key = append(key, dstIP...)
	key = append(key, encodePort(dstPort)...)
	return key
}

func (tk *Tracker) handleTimeout(ent *TrackEntry) {
	key := ent.Key
	requestCh, ok := <-tk.serviceChan
	if !ok {
		log.Printf("tracker is closed")
		return
	}

	request := ServiceRequest{
		Result: make(chan error),
		Fn: func(ctx context.Context) error {
			tk.EventC <- TrackerEvent{Type: TrackerEVTimeout, Entry: ent}
			tk.store.Delete(&TrackEntry{Key: key})
			return nil
		},
	}
	requestCh <- request
	if err := <-request.Result; err != nil {
		log.Printf("failed to untrack: %v", err)
	}

}

func (tk *Tracker) MarkSent(sentReceipt *TCPSYNSentReceipt) {
	key := buildKey(sentReceipt.SrcIP, sentReceipt.SrcPort, sentReceipt.Request.DstIP, sentReceipt.Request.DstPort)
	ent := &TrackEntry{Key: key, Value: sentReceipt}

	requestCh, ok := <-tk.serviceChan
	if !ok {
		log.Printf("tracker is closed")
		return
	}

	request := ServiceRequest{
		Result: make(chan error),
		Fn: func(ctx context.Context) error {
			tk.store.ReplaceOrInsert(ent)
			go func() {
				for range ent.Value.TimeoutC {
					tk.handleTimeout(ent)
				}
			}()
			return nil
		},
	}
	requestCh <- request
	if err := <-request.Result; err != nil {
		log.Printf("failed to mark sent: %v", err)
	}
}

func (tk *Tracker) MarkReceived(receivedPkt *PacketInfo) {
	if receivedPkt == nil || receivedPkt.Hdr == nil || receivedPkt.TCP == nil {
		log.Printf("received packet is nil, or some inner headers are nil")
		return
	}
	requestCh, ok := <-tk.serviceChan
	if !ok {
		log.Printf("tracker is closed")
		return
	}
	key := buildKey(receivedPkt.Hdr.Dst, int(receivedPkt.TCP.DstPort), receivedPkt.Hdr.Src, int(receivedPkt.TCP.SrcPort))
	receivedAt := time.Now()

	request := ServiceRequest{
		Fn: func(ctx context.Context) error {

			item := tk.store.Get(&TrackEntry{Key: key})
			if item == nil {
				log.Printf("no entry found for key: %v", key)
				return nil
			}

			ent, ok := item.(*TrackEntry)
			if !ok {
				panic("item is not a *TrackEntry")
			}

			ent.Value.ReceivedAt = receivedAt
			ent.Value.ReceivedPkt = receivedPkt
			ent.Value.ReceivedC <- receivedPkt
			tk.store.Delete(&TrackEntry{Key: key})

			return nil
		},
		Result: make(chan error),
	}
	requestCh <- request
	if err := <-request.Result; err != nil {
		log.Printf("failed to mark received: %v", err)
	}
}

type TCPSYNSentReceipt struct {
	SrcIP       net.IP
	SrcPort     int
	Request     *TCPSYNRequest
	SentAt      time.Time
	ReceivedAt  time.Time
	ReceivedPkt *PacketInfo
	TimeoutC    chan time.Time
	ReceivedC   chan *PacketInfo
}

func (receipt *TCPSYNSentReceipt) String() string {
	return fmt.Sprintf("at %s, %s:%d -> %s:%d", receipt.SentAt.Format(time.RFC3339Nano), receipt.SrcIP, receipt.SrcPort, receipt.Request.DstIP, receipt.Request.DstPort)
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

func buildTCPHdr(srcIP net.IP, srcPort int, dstIP net.IP, dstPort int, ttl int, syn bool, rst bool, seq uint32, ack uint32) (*ipv4.Header, []byte, error) {
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

	tcpLayer := &layers.TCP{
		SrcPort:    layers.TCPPort(srcPort),
		DstPort:    layers.TCPPort(dstPort),
		Seq:        seq,
		Ack:        ack,
		SYN:        syn,
		RST:        rst,
		DataOffset: uint8(tcpHdrLenNWords),
	}

	tcpLayer.SetNetworkLayerForChecksum(hdrLayer)
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		ComputeChecksums: true,
	}
	if err := gopacket.SerializeLayers(buf, opts, tcpLayer); err != nil {
		return nil, nil, fmt.Errorf("failed to serialize tcp layer: %v", err)
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
	return hdr, wb, nil
}

// length of tcp header, in unit of words (4 bytes)
// so, 5 words means 5 word * 4 bytes/word = 20 bytes
const tcpHdrLenNWords int = 5

func (sender *TCPSYNSender) Send(rawConn *ipv4.RawConn, request *TCPSYNRequest, tracker *Tracker) (*TCPSYNSentReceipt, error) {
	receipt := new(TCPSYNSentReceipt)
	receipt.TimeoutC = make(chan time.Time, 1)
	receipt.ReceivedC = make(chan *PacketInfo, 1)

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

	hdr, wb, err := buildTCPHdr(srcIP, localPort, dstIP, request.DstPort, ttl, true, false, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to build tcp syn: %v", err)
	}

	tracker.MarkSent(receipt)

	err = rawConn.WriteTo(hdr, wb, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to write syn to raw connection: %v", err)
	}
	receipt.SentAt = time.Now()
	timer := time.NewTimer(request.Timeout)

	go func() {
		defer tcpListener.Close()
		defer timer.Stop()
		defer close(receipt.TimeoutC)

		select {
		case time := <-timer.C:
			receipt.TimeoutC <- time
		case pkt, ok := <-receipt.ReceivedC:
			if ok && pkt != nil && pkt.Hdr != nil && pkt.TCP != nil {
				hdr, wb, err := buildTCPHdr(pkt.Hdr.Dst, int(pkt.TCP.DstPort), pkt.Hdr.Src, int(pkt.TCP.SrcPort), ttl, false, true, 1000, 0)
				if err != nil {
					log.Printf("failed to build tcp rst: %v", err)
					return
				}
				err = rawConn.WriteTo(hdr, wb, nil)
				if err != nil {
					log.Printf("failed to write rst to raw connection: %v", err)
				}
			}
			return
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

	trackerConfig := &TrackerConfig{}
	tracker := NewTracker(trackerConfig)
	tracker.Run(ctx)

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-tracker.EventC:
				if !ok {
					return
				}

				switch event.Type {
				case TrackerEVTimeout:
					log.Printf("timeout, it was: %s", event.Entry.Value.String())
				case TrackerEVReceived:
					log.Printf("got reply: %s, it was: %s", event.Entry.Value.ReceivedPkt.String(), event.Entry.Value.String())
				}
			}
		}
	}(ctx)

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

	synRequest := &TCPSYNRequest{
		DstIP:   dstIP,
		DstPort: dstPort,
		Timeout: 3 * time.Second,
	}
	_, err = sender.Send(rawConn, synRequest, tracker)
	if err != nil {
		log.Fatalf("failed to send tcp syn: %v", err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	log.Printf("received signal: %s", sig.String())
}
