package raw

import "encoding/base64"

// Package consists of types and functions for sending, as well as receiving, ICMP packets.

type RawBinary []byte

func (rb RawBinary) MarshalJSON() ([]byte, error) {
	return []byte("\"" + base64.StdEncoding.EncodeToString(rb) + "\""), nil
}

type WrappedPacket struct {
	Id   int
	Seq  int
	Src  string
	Dst  string
	Data RawBinary
}

type ICMPTransceiverConfig struct {
	Id          int
	Destination string
	InitialSeq  int
}

type ICMPTransceiver struct {
	config ICMPTransceiverConfig
}

func NewICMPTransceiver(config ICMPTransceiverConfig) *ICMPTransceiver {
	return &ICMPTransceiver{
		config: config,
	}
}
