package utils

import "net/http"

func GetRemoteAddr(r *http.Request) string {
	headersToTry := []string{
		"X-Forwarded-For",
		"x-forwarded-for",
		"X-Real-IP",
		"x-real-ip",
	}
	for _, headerKey := range headersToTry {
		if value := r.Header.Get(headerKey); value != "" {
			return value
		}
	}
	return r.RemoteAddr
}
