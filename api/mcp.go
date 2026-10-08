package handler

import (
	"net/http"

	"github.com/amxv/origo/internal/server"
)

// Handler exposes Origo's stateless MCP endpoint as a Vercel Go Function.
func Handler(w http.ResponseWriter, r *http.Request) {
	server.Handler().ServeHTTP(w, r)
}
