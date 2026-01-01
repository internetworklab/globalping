package raw

type GeneralICMPTransceiver interface {
	GetSender() <- chan chan ICMPSendRequest
	GetReceiver() <- chan ICMPReceiveReply
}

const ipv4HeaderLen int = 20
const ipv6HeaderLen int = 40
const udpHeaderLen int = 8
const headerSizeICMP int = 8
const protocolNumberICMPv4 int = 1
const protocolNumberICMPv6 int = 58
const icmpCodeFragmentationNeeded int = 4
