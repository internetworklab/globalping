package pinger

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type SimpleRemotePinger struct {
	Endpoint        string
	Request         SimplePingRequest
	ClientTLSConfig *tls.Config
	ExtraRequestHeader map[string]string
}

func (sp *SimpleRemotePinger) Ping(ctx context.Context) <-chan PingEvent {
	evChan := make(chan PingEvent)
	go func() {
		defer close(evChan)

		urlObj, err := url.Parse(sp.Endpoint)
		if err != nil {
			evChan <- PingEvent{Error: err}
			return
		}
		urlObj.RawQuery = sp.Request.ToURLValues().Encode()

		client := &http.Client{}
		if sp.ClientTLSConfig != nil {
			client.Transport = &http.Transport{
				TLSClientConfig: sp.ClientTLSConfig,
				Proxy:           http.ProxyFromEnvironment,
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", urlObj.String(), nil)
		if err != nil {
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
