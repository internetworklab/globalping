package main

import (
	"log"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func main() {
	payload := []byte("hello, world")
	log.Printf("[dbg] payload size: %d", len(payload))

	udpLayer := &layers.UDP{
		SrcPort: layers.UDPPort(12345),
		DstPort: layers.UDPPort(12346),
	}
	udpLayer.Payload = payload

	udpTotalLen := 8 + len(udpLayer.Payload)
	udpLayer.Length = uint16(udpTotalLen)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	payloadLayer := gopacket.Payload(udpLayer.Payload)
	err := payloadLayer.SerializeTo(buf, opts)
	if err != nil {
		log.Fatalf("failed to serialize payload layer of udp: %v", err)
	}
	err = udpLayer.SerializeTo(buf, opts)
	if err != nil {
		log.Fatalf("failed to serialize udp layer: %v", err)
	}
	wb := buf.Bytes()
	log.Printf("[dbg] udp packet size: %d, payload size: %d", len(wb), len(udpLayer.Payload))
}
