package utils

import "net/http"

func GetRemoteAddr(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xff := r.Header.Get("x-forwarded-for"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("x-real-ip"); xri != "" {
		return xri
	}
	return r.RemoteAddr
}
