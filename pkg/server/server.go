package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/amxv/go-mcp-template/pkg/oauth"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var transport = mcp.NewStreamableHTTPHandler(
	func(*http.Request) *mcp.Server {
		return makeServer()
	},
	&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
)

var oauthCache struct {
	sync.Mutex
	configuration [32]byte
	handler       http.Handler
}

func oauthHandler() (http.Handler, error) {
	// Cache the expensive HTTP clients between Vercel warm invocations, but
	// recognize changes to the environment in local development and tests.
	names := []string{"MCP_OAUTH_ISSUER", "MCP_OAUTH_OWNER_SECRET", "MCP_OAUTH_REDIS_REST_URL", "MCP_OAUTH_REDIS_REST_TOKEN", "MCP_OAUTH_CLIENTS_JSON"}
	var input strings.Builder
	for _, name := range names {
		value := os.Getenv(name)
		input.WriteString(name)
		input.WriteByte(0)
		input.WriteString(value)
		input.WriteByte(0)
	}
	fingerprint := sha256.Sum256([]byte(input.String()))
	oauthCache.Lock()
	defer oauthCache.Unlock()
	if oauthCache.handler != nil && oauthCache.configuration == fingerprint {
		return oauthCache.handler, nil
	}
	config, err := oauth.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	store, err := oauth.NewRedisStoreFromEnv(config.Issuer)
	if err != nil {
		return nil, err
	}
	server, err := oauth.New(config, store)
	if err != nil {
		return nil, err
	}
	oauthCache.configuration = fingerprint
	oauthCache.handler = server.Handler(transport)
	return oauthCache.handler, nil
}

// Handler selects exactly one authentication mode. The existing query-key
// flow is the default for backwards compatibility; OAuth never falls back
// to a query key, even if MCP_API_KEY is also configured.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex")

		mode := strings.TrimSpace(os.Getenv("MCP_AUTH_MODE"))
		if mode == "oauth" {
			handler, err := oauthHandler()
			if err != nil {
				http.Error(w, "OAuth endpoint not configured", http.StatusServiceUnavailable)
				return
			}
			handler.ServeHTTP(w, r)
			return
		}
		if mode != "" && mode != "key" {
			http.Error(w, "Invalid authentication mode", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/mcp" && r.URL.Path != "/api/mcp" {
			http.NotFound(w, r)
			return
		}

		secret := strings.TrimSpace(os.Getenv("MCP_API_KEY"))
		if secret == "" {
			http.Error(w, "MCP endpoint not configured", http.StatusServiceUnavailable)
			return
		}

		keys := r.URL.Query()["key"]
		if len(keys) != 1 || !validKey(secret, keys[0]) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		transport.ServeHTTP(w, r)
	})
}

func validKey(expected, supplied string) bool {
	if supplied == "" {
		return false
	}
	want := sha256.Sum256([]byte(expected))
	got := sha256.Sum256([]byte(supplied))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}
