package raw

import (
	"encoding/json"
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

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

func extractPacketInfoFromOriginIP4(originIPPacketRaw []byte, basePort int) (identifier *PacketIdentifier, err error) {
	identifier = new(PacketIdentifier)

	if originIPPacketRaw == nil {
		return nil, fmt.Errorf("origin IP packet raw is nil")
	}

	originPacket := gopacket.NewPacket(originIPPacketRaw, layers.LayerTypeIPv4, gopacket.Default)
	if originPacket == nil {
		err = fmt.Errorf("failed to create/decode origin ip packet")
		return nil, err
	}

	originIPLayer := originPacket.Layer(layers.LayerTypeIPv4)
	if originIPLayer == nil {
		err = fmt.Errorf("failed to extract origin ip layer")
		return nil, err
	}

	originIPPacket, ok := originIPLayer.(*layers.IPv4)
	if !ok {
		err = fmt.Errorf("failed to cast origin ip layer to origin ip packet")
		return nil, err
	}

	identifier.IPProto = int(originIPPacket.Protocol)

	if originIPPacket.Protocol == layers.IPProtocolICMPv4 {
		originICMPLayer := originPacket.Layer(layers.LayerTypeICMPv4)
		if originICMPLayer == nil {
			err = fmt.Errorf("failed to extract origin icmp layer")
			return nil, err
		}

		originICMPPacket, ok := originICMPLayer.(*layers.ICMPv4)
		if !ok {
			err = fmt.Errorf("failed to cast origin icmp layer to origin icmp packet")
			return nil, err
		}

		identifier.Id = int(originICMPPacket.Id)
		identifier.Seq = int(originICMPPacket.Seq)
		return nil, err
	} else if originIPPacket.Protocol == layers.IPProtocolUDP {
		originUDPLayer := originPacket.Layer(layers.LayerTypeUDP)
		if originUDPLayer == nil {
			err = fmt.Errorf("failed to extract origin udp layer")
			return nil, err
		}

		originUDPPacket, ok := originUDPLayer.(*layers.UDP)
		if !ok {
			err = fmt.Errorf("failed to cast origin udp layer to origin udp packet")
			return nil, err
		}
		identifier.Id = int(originUDPPacket.SrcPort)
		identifier.Seq = int(originUDPPacket.DstPort) - basePort
		return identifier, err
	} else {
		err = fmt.Errorf("unknown origin ip protocol: %d", originIPPacket.Protocol)
		return identifier, err
	}
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

		subIdentifier, err := extractPacketInfoFromOriginIP4(icmpPacket.Payload, baseDstPort)
		if err != nil {
			return nil, fmt.Errorf("failed to extract origin packet info from icmp error message: %w", err)
		}

		identifier.Id = subIdentifier.Id
		identifier.Seq = subIdentifier.Seq
		identifier.IPProto = subIdentifier.IPProto
		if identifier.IPProto == int(layers.IPProtocolUDP) {
			identifier.LastHop = icmpPacket.TypeCode.Code() == layers.ICMPv4CodePort
		}

		return identifier, err
	} else if icmpPacket.TypeCode.Type() == layers.ICMPv4TypeTimeExceeded {
		subIdentifier, err := extractPacketInfoFromOriginIP4(icmpPacket.Payload, baseDstPort)
		if err != nil {
			return nil, fmt.Errorf("failed to extract origin packet info from icmp error message: %w", err)
		}

		identifier.IPProto = subIdentifier.IPProto
		identifier.LastHop = false
		identifier.Id = subIdentifier.Id
		identifier.Seq = subIdentifier.Seq
		return identifier, err
	} else {
		err = fmt.Errorf("unknown icmp type: %d", icmpPacket.TypeCode.Type())
		return identifier, err
	}
}

func extractPacketInfoFromOriginIP6(originIPPacketRaw []byte, baseDstPort int) (identifier *PacketIdentifier, err error) {
	identifier = new(PacketIdentifier)

	packet := gopacket.NewPacket(originIPPacketRaw, layers.LayerTypeIPv6, gopacket.Default)
	if packet == nil {
		err = fmt.Errorf("failed to create/decode origin ip6 packet")
		return nil, err
	}

	ip6Layer := packet.Layer(layers.LayerTypeIPv6)
	if ip6Layer == nil {
		err = fmt.Errorf("failed to extract ip6 layer")
		return nil, err
	}

	ip6Packet, ok := ip6Layer.(*layers.IPv6)
	if !ok {
		err = fmt.Errorf("failed to cast ip6 layer to ip6 packet")
		return nil, err
	}

	identifier.IPProto = int(ip6Packet.NextHeader)
	switch ip6Packet.NextHeader {
	case layers.IPProtocolICMPv6:
		originICMPMsg, err := icmp.ParseMessage(int(layers.IPProtocolICMPv6), ip6Packet.Payload)
		if err != nil {
			err = fmt.Errorf("failed to parse origin icmp message: %w", err)
			return nil, err
		}

		if originICMPMsg.Type != ipv6.ICMPTypeEchoReply {
			err = fmt.Errorf("unexpected icmpv6 type: %v", originICMPMsg.Type)
			return nil, err
		}

		originICMPEchoMsg, ok := originICMPMsg.Body.(*icmp.Echo)
		if !ok {
			return nil, fmt.Errorf("failed to cast origin icmp message body to *icmp.Echo")
		}

		identifier.Id = int(originICMPEchoMsg.ID)
		identifier.Seq = int(originICMPEchoMsg.Seq)

		return identifier, nil
	case layers.IPProtocolUDP:
		udpLayer := packet.Layer(layers.LayerTypeUDP)
		if udpLayer == nil {
			err = fmt.Errorf("failed to extract udp layer from origin ip6 packet")
			return nil, err
		}

		udpPacket, ok := udpLayer.(*layers.UDP)
		if !ok {
			err = fmt.Errorf("failed to cast udp layer to udp packet")
			return nil, err
		}

		identifier.Id = int(udpPacket.SrcPort)
		identifier.Seq = int(udpPacket.DstPort) - baseDstPort
	default:
		err = fmt.Errorf("unknown ip6 next header: %d", ip6Packet.NextHeader)
		return nil, err
	}

	return identifier, nil
}
