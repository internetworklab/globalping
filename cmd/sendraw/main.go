package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func sendTracertUDP() {
	ipProtoICMP := fmt.Sprintf("%d", int(layers.IPProtocolICMPv4))
	c, err := net.ListenPacket("ip4:"+ipProtoICMP, "0.0.0.0")
	if err != nil {
		log.Fatalf("failed to listen on packet:ip4:%s: %v", ipProtoICMP, err)
	}
	defer c.Close()
	r, err := ipv4.NewRawConn(c)
	if err != nil {
		log.Fatalf("failed to create RawConn over ip4 PacketConn: %v", err)
	}

	udpLayer := &layers.UDP{
		SrcPort: layers.UDPPort(12345),
		DstPort: layers.UDPPort(12346),
		Length:  8, // 8 bytes for minimal udp header
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	err = gopacket.SerializeLayers(buf, opts, udpLayer)
	if err != nil {
		log.Fatalf("failed to serialize udp layer: %v", err)
	}
	udpBytes := buf.Bytes()

	iph := &ipv4.Header{
		Version:  ipv4.Version,
		Len:      ipv4.HeaderLen,
		TotalLen: ipv4.HeaderLen + len(udpBytes),
		TTL:      1,
		Protocol: int(layers.IPProtocolUDP),
		Dst:      net.IP{8, 8, 4, 4},
		Flags:    ipv4.DontFragment,
	}

	var cm *ipv4.ControlMessage = nil

	if err := r.WriteTo(iph, udpBytes, cm); err != nil {
		log.Fatalf("failed to send raw ip packet: %v", err)
	}
}

func sendTracertUDP6() {
	listenAddr := fmt.Sprintf("[::]:%d", 0)
	ln, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		log.Fatalf("failed to listen on udp: %v", err)
	}
	defer ln.Close()

	udpAddr, ok := ln.LocalAddr().(*net.UDPAddr)
	if !ok {
		panic("failed to cast local address to *net.UDPAddr")
	}
	log.Printf("source port will be: %d", udpAddr.Port)

	p := ipv6.NewPacketConn(ln)
	p.SetHopLimit(1)

	dst := net.ParseIP("2a00:1450:4001:813::200e")
	dstPort := 13325
	dstAddr := &net.UDPAddr{
		IP:   dst,
		Port: dstPort,
	}
	var cm *ipv6.ControlMessage = nil
	n, err := p.WriteTo(nil, cm, dstAddr)
	if err != nil {
		log.Fatalf("failed to write to connection: %v", err)
	}
	log.Printf("wrote %d bytes to %s", n, dstAddr.String())

	// send a long ttl packet
	dstPort = 34433+63
	dstAddr = &net.UDPAddr{
		IP:   dst,
		Port: dstPort,
	}
	p.SetHopLimit(63)
	n, err = p.WriteTo([]byte("hello"), cm, dstAddr)
	if err != nil {
		log.Fatalf("failed to write second packet to the connection: %v", err)
	}
	log.Printf("wrote %d bytes to %s", n, dstAddr.String())
}

func sendICMPEcho() {
	var err error = nil
	ipProtoICMP := fmt.Sprintf("%d", int(layers.IPProtocolICMPv4))
	c, err := net.ListenPacket("ip4:"+ipProtoICMP, "0.0.0.0")
	if err != nil {
		log.Fatalf("failed to listen on packet:ip4:%s: %v", ipProtoICMP, err)
	}
	defer c.Close()
	r, err := ipv4.NewRawConn(c)
	if err != nil {
		log.Fatalf("failed to create RawConn over ip4 PacketConn: %v", err)
	}

	icmpId := 1000 + rand.Intn(65536-1000-1)
	log.Printf("Using ICMP ID: %d", icmpId)

	icmpSeq := 1
	log.Printf("Using ICMP Sequence: %d", icmpSeq)

	var wb []byte = nil
	wm := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   icmpId,
			Data: nil,
			Seq:  icmpSeq,
		},
	}
	wb, err = wm.Marshal(nil)
	if err != nil {
		log.Fatalf("failed to marshal icmp message: %v", err)
	}

	log.Printf("ICMP message length: %d", len(wb))

	dst := net.IP{8, 8, 4, 4}
	log.Printf("Sending ICMP echo to %s", dst.String())

	ttl := 1
	log.Printf("Using TTL: %d", ttl)

	iph := &ipv4.Header{
		Version:  ipv4.Version,
		Len:      ipv4.HeaderLen,
		TotalLen: ipv4.HeaderLen + len(wb),
		TTL:      ttl,
		Protocol: int(layers.IPProtocolICMPv4),
		Dst:      dst,
		Flags:    ipv4.DontFragment,
	}

	var cm *ipv4.ControlMessage = nil

	if err := r.WriteTo(iph, wb, cm); err != nil {
		log.Fatalf("failed to send raw ip packet: %v", err)
	}
}

func main() {
	log.Printf("--------------------------------")
	log.Printf("Sending raw UDP/IP packet for traceroute ...")
	sendTracertUDP()
	log.Printf("UDP is sent, waiting for 3 seconds to send next packet (ICMP echo) ...")
	time.Sleep(1 * time.Second)
	log.Printf("--------------------------------")
	log.Printf("Sending raw ICMP echo packet ...")
	sendICMPEcho()
	log.Printf("ICMP is sent, waiting for 3 seconds to send next packet (UDP/IP traceroute) ...")
	time.Sleep(1 * time.Second)
	log.Printf("--------------------------------")
	log.Printf("Sending raw UDP6/IPv6 packet for traceroute ...")
	sendTracertUDP6()
	log.Println("All done.")
}
