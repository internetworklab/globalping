package pinger

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
)

type SimpleRemotePinger struct {
	NodeName           string
	Endpoint           string
	Request            SimplePingRequest
	ClientTLSConfig    *tls.Config
	ExtraRequestHeader map[string]string
	QUICClient         *http.Client
}

func (sp *SimpleRemotePinger) getDefaultClient() *http.Client {
	client := &http.Client{}
	if sp.ClientTLSConfig != nil {
		client.Transport = &http.Transport{
			TLSClientConfig: sp.ClientTLSConfig,
		}
	}
	return client
}

func (sp *SimpleRemotePinger) Ping(ctx context.Context) <-chan PingEvent {
	// return mockPing(ctx)
	evChan := make(chan PingEvent)
	go func() {
		defer close(evChan)

		urlStr := fmt.Sprintf("http://%s/simpleping", sp.NodeName)
		client := sp.getDefaultClient()
		if sp.QUICClient != nil {
			urlObj, err := url.Parse(urlStr)
			if err != nil {
				log.Printf("failed to parse endpoint: %v", err)
				evChan <- PingEvent{Error: err}
				return
			}
			urlObj.RawQuery = sp.Request.ToURLValues().Encode()
			urlStr = urlObj.String()
			client = sp.QUICClient
		} else {
			urlObj, err := url.Parse(sp.Endpoint)
			if err != nil {
				log.Printf("failed to parse endpoint: %v", err)
				evChan <- PingEvent{Error: err}
				return
			}
			urlObj.RawQuery = sp.Request.ToURLValues().Encode()
			urlStr = urlObj.String()
		}

		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			log.Printf("failed to create request: %v", err)
			evChan <- PingEvent{Error: err}
			return
		}

		if sp.ExtraRequestHeader != nil {
			for k, v := range sp.ExtraRequestHeader {
				req.Header.Set(k, v)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("failed to send request to %s: %v", urlStr, err)
			evChan <- PingEvent{Error: err}
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if err := scanner.Err(); err != nil {
				evChan <- PingEvent{Error: err}
				return
			}

			line := scanner.Bytes()

			pingEVObj := new(PingEvent)
			if err := json.Unmarshal(line, pingEVObj); err != nil {
				if pingEVObj.Err != nil {
					pingEVObj.Error = fmt.Errorf("%s", *pingEVObj.Err)
				}
				evChan <- PingEvent{Error: err}
				return
			}
			evChan <- *pingEVObj
		}

	}()

	return evChan
}
