package handler

import (
	"encoding/json"
	"net/http"

	pkgutils "example.com/rbmq-demo/pkg/utils"
)

func NewVersionHandler(sharedCtx *pkgutils.GlobalSharedContext) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(sharedCtx.BuildVersion)
	})
}
