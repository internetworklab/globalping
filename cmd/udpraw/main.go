package main

import (
	"fmt"
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func main() {

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		ComputeChecksums: true,
	} // See SerializeOptions for more details.

	udp := &layers.UDP{
		SrcPort: layers.UDPPort(1234),
		DstPort: layers.UDPPort(1235),
		Length:  8, // the header and the payload are all included
	}

	ip6 := &layers.IPv6{
		SrcIP:      net.ParseIP("::1"),
		DstIP:      net.ParseIP("::2"),
		NextHeader: layers.IPProtocolUDP,
	}

	udp.SetNetworkLayerForChecksum(ip6)

	err := udp.SerializeTo(buf, opts)
	if err != nil {
		panic(err)
	}
	fmt.Println(buf.Bytes())
}
