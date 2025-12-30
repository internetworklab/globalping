package main

import (
	"fmt"
	"log"
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/ipv4"
)

func main() {
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
	}

	var cm *ipv4.ControlMessage = nil

	if err := r.WriteTo(iph, udpBytes, cm); err != nil {
		log.Fatalf("failed to send raw ip packet: %v", err)
	}
}
