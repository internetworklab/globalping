package main

import (
	"net"
	"net/http"
	"time"

	"log"

	"encoding/json"

	pkgauth "example.com/rbmq-demo/pkg/auth"
	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func main() {
	secret := []byte("my_secret_key")

	tokenObject := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:   "globalping-hub",
		Subject:  "administrator",
		IssuedAt: jwt.NewNumericDate(time.Now()),
		ID:       uuid.New().String(),
	})
	tokenString, err := tokenObject.SignedString(secret)
	if err != nil {
		log.Fatalf("failed to sign token: %v", err)
	}
	log.Printf("Token: %s", tokenString)

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		token := r.Context().Value(pkgutils.CtxKeyJWTToken).(*jwt.Token)
		if token != nil {
			json.NewEncoder(w).Encode(token.Claims)
			return
		}
		json.NewEncoder(w).Encode(pkgutils.ErrorResponse{Error: "No token found"})
	})

	handler = pkgauth.WithJWTAuth(handler, secret)

	server := http.Server{
		Handler: handler,
	}

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{
		Port: 0,
	})
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	log.Printf("Listening on port %d", listener.Addr().(*net.TCPAddr).Port)

	err = server.Serve(listener)
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
