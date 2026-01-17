package main

import (
	"context"
	"log"
	"net/netip"
	"os"
	"time"

	"encoding/json"

	pkgdnsprobe "example.com/rbmq-demo/pkg/dnsprobe"
)

func main() {
	addrport, err := netip.ParseAddrPort("8.8.4.4:53")
	if err != nil {
		log.Fatalf("failed to parse addrport: %v", err)
	}

	targets := []string{
		"www.google.com",
		"www.baidu.com",
		"www.12023bingo2398sjdsfjoidsjo.com",
	}
	queryTypes := []pkgdnsprobe.DNSQueryType{
		pkgdnsprobe.DNSQueryTypeA,
		pkgdnsprobe.DNSQueryTypeAAAA,
		pkgdnsprobe.DNSQueryTypeCNAME,
	}

	for _, target := range targets {
		for _, queryType := range queryTypes {
			parameter := pkgdnsprobe.LookupParameter{
				AddrPort:  addrport,
				Target:    target,
				Timeout:   3000 * time.Millisecond,
				Transport: pkgdnsprobe.TransportUDP,
				QueryType: queryType,
			}
			log.Printf("looking up type %s dns for target %s", queryType, target)
			queryResult, err := pkgdnsprobe.LookupDNS(context.Background(), parameter)
			if err != nil {
				log.Printf("failed to lookup type %s dns for target %s: %v", queryType, target, err)
			}
			queryResult, err = queryResult.PreStringify()
			if err != nil {
				log.Printf("failed to pre-stringify query result: %v", err)
			}
			json.NewEncoder(os.Stdout).Encode(queryResult)
		}
	}

}
