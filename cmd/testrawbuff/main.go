package main

import (
	"encoding/json"
	"log"
	"os"

	pkgraw "example.com/rbmq-demo/pkg/raw"
)

func main() {
	pkgTest := pkgraw.WrappedPacket{
		Id:   1,
		Seq:  1,
		Src:  "127.0.0.1",
		Dst:  "127.0.0.1",
		Data: pkgraw.RawBinary{0x01, 0x02, 0x03, 0x04},
	}
	err := json.NewEncoder(os.Stdout).Encode(pkgTest)
	if err != nil {
		log.Fatalf("failed to marshal packet: %v", err)
	}
}
