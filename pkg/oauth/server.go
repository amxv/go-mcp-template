package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	codeTTL    = 5 * time.Minute
	accessTTL  = 5 * time.Minute
	grantTTL   = 30 * 24 * time.Hour
	sessionTTL = 30 * time.Minute
	pendingTTL = 10 * time.Minute
)

type Server struct {
	config Config
	store  Store
	client *http.Client
}

// Principal is verified on every request, not carried over an MCP session.
type Principal struct {
	Subject  string
	ClientID string
	Scope    string
}

type principalKey struct{}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

func New(config Config, store Store) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("OAuth requires durable Redis state")
	}
	client := config.HTTPClient
	if client == nil {
		client = chatGPTHTTPClient()
	}
	return &Server{config: config, store: store, client: client}, nil
}

func (s *Server) Resource() string { return s.config.Issuer + ResourcePath }

func (s *Server) validHost(r *http.Request) bool {
	host, _ := s.config.Host()
	if r.Host != host || (r.URL.Scheme != "" && r.URL.Scheme != "https") || (r.URL.Host != "" && r.URL.Host != host) || r.URL.RawPath != "" {
		return false
	}
	for name, expected := range map[string]string{"X-Forwarded-Host": host, "X-Forwarded-Proto": "https"} {
		values := r.Header.Values(name)
		if len(values) > 1 || (len(values) == 1 && values[0] != expected) {
			return false
		}
	}
	return len(r.URL.RawQuery) <= 8192
}

// Handler serves both authorization endpoints and protected MCP transport.
// All URL discovery and token operations are handled by the same Go Function.
func (s *Server) Handler(mcp http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		if !s.validHost(r) {
			http.Error(w, "Invalid OAuth host", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case ResourceMetadata, ResourceMetadata + ResourcePath:
			if !getOnly(w, r) {
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"resource": s.Resource(), "authorization_servers": []string{s.config.Issuer},
				"scopes_supported": []string{Scope}, "bearer_methods_supported": []string{"header"},
			})
		case AuthorizationMeta:
			if !getOnly(w, r) {
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"issuer": s.config.Issuer, "authorization_endpoint": s.config.Issuer + "/oauth/authorize",
				"token_endpoint": s.config.Issuer + "/oauth/token", "response_types_supported": []string{"code"},
				"response_modes_supported": []string{"query"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
				"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"},
				"authorization_response_iss_parameter_supported": true, "client_id_metadata_document_supported": true,
				"scopes_supported": []string{Scope},
			})
		case "/oauth/authorize":
			s.authorize(w, r)
		case "/oauth/token":
			s.token(w, r)
		case "/oauth/login", "/oauth/consent", "/oauth/grants", "/oauth/logout", "/oauth/revoke":
			s.browser(w, r)
		case ResourcePath:
			s.protect(mcp).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
}

func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func validToken(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("-._~", c)) {
			return false
		}
	}
	return true
}

func checkPKCE(verifier, challenge string) bool {
	if !validVerifier(verifier) || !validToken(challenge) {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(digest[:])), []byte(challenge)) == 1
}

func singleValues(values url.Values) bool {
	for _, v := range values {
		if len(v) != 1 {
			return false
		}
	}
	return true
}

func parseForm(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		oauthError(w, http.StatusMethodNotAllowed, "invalid_request")
		return nil, false
	}
	if r.URL.RawQuery != "" || len(r.Header.Values("Authorization")) != 0 {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	params, err := url.ParseQuery(string(data))
	if err != nil || !singleValues(params) {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	return params, true
}

func (s *Server) staticClient(id string) (Client, bool) {
	if id == ChatGPTClientID {
		return Client{ID: id, Name: "ChatGPT", RedirectURIs: []string{ChatGPTRedirect}}, true
	}
	for _, client := range s.config.Clients {
		if id == client.ID {
			return client, true
		}
	}
	return Client{}, false
}

type clientMetadata struct {
	Client
	Responses   []string `json:"response_types"`
	Grants      []string `json:"grant_types"`
	AuthMethods []string `json:"token_endpoint_auth_methods_supported"`
	AuthMethod  string   `json:"token_endpoint_auth_method"`
}

var chatGPTCache struct {
	sync.Mutex
	validUntil time.Time
	valid      bool
}

func (s *Server) resolveClient(ctx context.Context, id string) (Client, error) {
	client, ok := s.staticClient(id)
	if !ok {
		return Client{}, errors.New("unknown client")
	}
	if id != ChatGPTClientID {
		return client, nil
	}
	chatGPTCache.Lock()
	defer chatGPTCache.Unlock()
	if time.Now().Before(chatGPTCache.validUntil) {
		if chatGPTCache.valid {
			return client, nil
		}
		return Client{}, errors.New("ChatGPT metadata unavailable")
	}
	chatGPTCache.valid = false
	chatGPTCache.validUntil = time.Now().Add(5 * time.Second)
	allowed, err := s.store.AllowClientMetadata(ctx)
	if err != nil || !allowed {
		return Client{}, errors.New("ChatGPT metadata fetch capacity unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ChatGPTClientID, nil)
	if err != nil {
		return Client{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return Client{}, errors.New("ChatGPT client metadata unavailable")
	}
	defer resp.Body.Close()
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || resp.StatusCode != http.StatusOK || media != "application/json" {
		return Client{}, errors.New("invalid ChatGPT client metadata response")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32769))
	if err != nil || len(data) > 32768 {
		return Client{}, errors.New("invalid ChatGPT client metadata size")
	}
	var doc clientMetadata
	if strictJSON(data, &doc) != nil || doc.ID != ChatGPTClientID || doc.Name == "" || len(doc.RedirectURIs) != 1 || doc.RedirectURIs[0] != ChatGPTRedirect || !contains(doc.Responses, "code") || !contains(doc.Grants, "authorization_code") || !contains(doc.Grants, "refresh_token") {
		return Client{}, errors.New("invalid ChatGPT client metadata")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields["client_id"] == nil || fields["client_name"] == nil || fields["redirect_uris"] == nil || fields["response_types"] == nil || fields["grant_types"] == nil || (fields["token_endpoint_auth_method"] == nil && fields["token_endpoint_auth_methods_supported"] == nil) {
		return Client{}, errors.New("ChatGPT client metadata needs exact fields")
	}
	methods := doc.AuthMethods
	if len(methods) == 0 {
		methods = []string{doc.AuthMethod}
	}
	if !contains(methods, "none") {
		return Client{}, errors.New("ChatGPT metadata does not support public clients")
	}
	chatGPTCache.valid = true
	chatGPTCache.validUntil = time.Now().Add(5 * time.Minute)
	return client, nil
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// The only network destination is a fixed trusted hostname, not a supplied
// client_id URL. Prevent redirects, proxy use, and private-address DNS answers.
func chatGPTHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 16 << 10, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != "chatgpt.com" || port != "443" {
			return nil, ErrUnavailable
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 || len(ips) > 32 {
			return nil, ErrUnavailable
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, ErrUnavailable
			}
		}
		for _, ip := range ips {
			if conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port)); err == nil {
				return conn, nil
			}
		}
		return nil, ErrUnavailable
	}}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}
}
