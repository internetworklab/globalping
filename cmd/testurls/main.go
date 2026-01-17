package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	pkgdnsprobe "example.com/rbmq-demo/pkg/dnsprobe"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if targets := r.URL.Query()["targets"]; targets != nil {
			for i, target := range targets {
				dnsProbeTarget := new(pkgdnsprobe.LookupParameter)
				err := json.Unmarshal([]byte(target), dnsProbeTarget)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to parse target: %v", err), http.StatusBadRequest)
					return
				}

				j, err := json.Marshal(dnsProbeTarget)
				if err != nil {
					http.Error(w, fmt.Sprintf("failed to marshal target: %v", err), http.StatusInternalServerError)
					return
				}

				fmt.Fprintf(w, "[%d]: %s\n", i, string(j))
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	})
	http.ListenAndServe(":8787", nil)
}
