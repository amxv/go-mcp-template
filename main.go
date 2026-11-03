package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/amxv/go-mcp-template/pkg/server"
)

// OAuth's well-known discovery paths need native routing: Vercel does not
// rewrite /.well-known. Deploy this server with vercel.oauth.json rather
// than the default api/mcp.go Go Function configuration.
func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	app := &http.Server{
		Addr:              ":" + port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      110 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	log.Print("MCP server listening")
	if err := app.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("MCP server stopped: ", err)
	}
}
