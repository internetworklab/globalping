package auth

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/golang-jwt/jwt/v5"
)

func WithJWTAuth(handler http.Handler, secret []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString := r.Header.Get("Authorization")
		tokenString = strings.TrimPrefix(tokenString, "Bearer ")
		tokenString = strings.TrimPrefix(tokenString, "bearer ")

		rejectWithErr := func() {
			unAuthErr := pkgutils.ErrorResponse{Error: "Unauthorized"}
			remote := pkgutils.GetRemoteAddr(r)
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(unAuthErr)
			log.Printf("Remote peer %s is rejected by JWT middleware", remote)
		}

		if tokenString == "" {
			rejectWithErr()
			return
		}

		if secret == nil {
			log.Printf("WARN: JWT middleware is applied but no JWT secret is found in context")
			handler.ServeHTTP(w, r)
			return
		}

		if len(secret) < 4 {
			log.Printf("WARN: JWT middleware is applied but JWT secret is too short, is that reliable ? (")
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
			// in future, one should determine which key to use base on the 'kid' (key ID) claim of the token
			// for now, return a fixed key is enough, becuase the people who use our service can be counted on one hand.
			return secret, nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

		if err != nil {
			rejectWithErr()
			return
		}

		if !token.Valid {
			rejectWithErr()
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, pkgutils.CtxKeyJWTToken, token)
		r = r.WithContext(ctx)

		handler.ServeHTTP(w, r)
	})
}
