package main

import (
	"context"
	"log"
	"os"

	"encoding/json"

	pkgdnsprobe "example.com/rbmq-demo/pkg/dnsprobe"
)

func main() {
	addrport := "8.8.4.4:53"

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
				TimeoutMs: 3000,
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
