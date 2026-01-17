package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	pkgdnsprobe "example.com/rbmq-demo/pkg/dnsprobe"
	"github.com/google/uuid"
)

func serve() chan net.Listener {
	listenerChan := make(chan net.Listener, 1)
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		log.Printf("listening on port %d", tcpAddr.Port)
		listenerChan <- listener
	} else {
		log.Printf("listening on %s", listener.Addr())
	}
	server := &http.Server{}
	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if targets := r.URL.Query()["dnsTarget"]; targets != nil {
			for _, target := range targets {
				dnsProbeTarget := new(pkgdnsprobe.LookupParameter)
				err := json.Unmarshal([]byte(target), dnsProbeTarget)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to parse target: %v", err), http.StatusBadRequest)
					return
				}

				queryResult, err := pkgdnsprobe.LookupDNS(r.Context(), *dnsProbeTarget)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to lookup DNS: %v", err), http.StatusInternalServerError)
					return
				}

				queryResult, err = queryResult.PreStringify()
				if err != nil {
					log.Fatalf("failed to pre-stringify query result: %v", err)
				}

				json.NewEncoder(w).Encode(queryResult)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	})
	server.Handler = serveMux
	go server.Serve(listener)
	return listenerChan
}

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

	responses := make(map[string]*pkgdnsprobe.QueryResult)
	urlValues := url.Values{}
	for _, target := range targets {
		for _, queryType := range queryTypes {
			queryCorrId := uuid.New().String()

			parameter := pkgdnsprobe.LookupParameter{
				AddrPort:      addrport,
				Target:        target,
				TimeoutMs:     3000,
				Transport:     pkgdnsprobe.TransportUDP,
				QueryType:     queryType,
				CorrelationID: queryCorrId,
			}
			j, err := json.Marshal(parameter)
			if err != nil {
				log.Fatalf("failed to marshal parameter: %v", err)
			}
			urlValues.Add("dnsTarget", string(j))
			responses[queryCorrId] = nil
		}
	}

	listenerChan := serve()
	listener := <-listenerChan
	port := listener.Addr().(*net.TCPAddr).Port
	endpoint := fmt.Sprintf("http://localhost:%d", port)
	urlObj, err := url.Parse(endpoint)
	if err != nil {
		log.Fatalf("failed to parse endpoint: %v", err)
	}
	urlObj.RawQuery = urlValues.Encode()
	client := &http.Client{}
	request, err := http.NewRequest("GET", urlObj.String(), nil)
	if err != nil {
		log.Fatalf("failed to create request: %v", err)
	}
	resp, err := client.Do(request)
	if err != nil {
		log.Fatalf("failed to send request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("expected status OK, got %d", resp.StatusCode)
	}

	log.Printf("got responses:")
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if err := scanner.Err(); err != nil {
			log.Fatalf("failed to read response body: %v", err)
		}

		queryResult := new(pkgdnsprobe.QueryResult)
		if err := json.Unmarshal(scanner.Bytes(), queryResult); err != nil {
			log.Fatalf("failed to unmarshal response: %v", err)
		}

		responses[queryResult.CorrelationID] = queryResult
		j, _ := json.Marshal(queryResult)
		log.Printf("got response: %s", string(j))
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
}
