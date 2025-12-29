package raw

type GeneralICMPTransceiver interface {
	GetSender() chan<- ICMPSendRequest
	GetReceiver() chan<- chan ICMPReceiveReply
}

const ipv4HeaderLen int = 20
const ipv6HeaderLen int = 40
const headerSizeICMP int = 8
const protocolNumberICMPv4 int = 1
const protocolNumberICMPv6 int = 58
