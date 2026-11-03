// Package oauth is a single-owner OAuth 2.1 authorization server for the MCP
// template. It deliberately does not accept arbitrary client metadata URLs.
package oauth

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode"
)

const (
	Scope             = "mcp:access"
	ChatGPTClientID   = "https://chatgpt.com/oauth/client.json"
	ChatGPTRedirect   = "https://chatgpt.com/connector_platform_oauth_redirect"
	ResourcePath      = "/mcp"
	ResourceMetadata  = "/.well-known/oauth-protected-resource"
	AuthorizationMeta = "/.well-known/oauth-authorization-server"
)

type Client struct {
	ID           string   `json:"client_id"`
	Name         string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

type Config struct {
	Issuer      string
	OwnerSecret string
	Clients     []Client
	// HTTPClient is only used to fetch ChatGPT's single, fixed client metadata URL.
	// The default client never follows redirects and restricts DNS destinations.
	HTTPClient *http.Client
}

func ConfigFromEnv() (Config, error) {
	c := Config{Issuer: os.Getenv("MCP_OAUTH_ISSUER"), OwnerSecret: os.Getenv("MCP_OAUTH_OWNER_SECRET")}
	if raw := os.Getenv("MCP_OAUTH_CLIENTS_JSON"); raw != "" {
		if len(raw) > 16384 || strictJSON([]byte(raw), &c.Clients) != nil {
			return Config{}, errors.New("invalid MCP_OAUTH_CLIENTS_JSON")
		}
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if _, err := c.Host(); err != nil {
		return err
	}
	if len(c.OwnerSecret) < 32 || len(c.OwnerSecret) > 1024 || strings.TrimSpace(c.OwnerSecret) != c.OwnerSecret || strings.ContainsFunc(c.OwnerSecret, unicode.IsControl) {
		return errors.New("MCP_OAUTH_OWNER_SECRET must be a random secret of at least 32 characters")
	}
	if len(c.Clients) > 16 {
		return errors.New("too many OAuth clients")
	}
	seen := map[string]bool{ChatGPTClientID: true}
	for _, client := range c.Clients {
		if !bounded(client.ID, 512) || !bounded(client.Name, 128) || len(client.RedirectURIs) == 0 || len(client.RedirectURIs) > 8 || seen[client.ID] {
			return errors.New("invalid or duplicate OAuth client")
		}
		seen[client.ID] = true
		redirects := map[string]bool{}
		for _, raw := range client.RedirectURIs {
			if !validRedirect(raw) || redirects[raw] {
				return errors.New("invalid or duplicate OAuth redirect URI")
			}
			redirects[raw] = true
		}
	}
	return nil
}

func (c Config) Host() (string, error) {
	u, err := url.Parse(c.Issuer)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Host != strings.ToLower(u.Host) || c.Issuer != "https://"+u.Host || net.ParseIP(u.Hostname()) != nil {
		return "", errors.New("MCP_OAUTH_ISSUER must be an exact lowercase public HTTPS origin without a port or path")
	}
	if !validHost(u.Hostname()) {
		return "", errors.New("invalid OAuth issuer host")
	}
	return u.Host, nil
}

func validHost(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, end := range []string{".local", ".localhost", ".internal", ".test", ".invalid", ".onion"} {
		if strings.HasSuffix(host, end) {
			return false
		}
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func validRedirect(raw string) bool {
	if !bounded(raw, 2048) || strings.ContainsAny(raw, "\\\t \r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || !validHost(u.Hostname()) || u.Port() != "" || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" || u.ForceQuery {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for _, name := range []string{"code", "error", "state", "iss"} {
		if q.Has(name) {
			return false
		}
	}
	return true
}

func bounded(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func (c Client) Allows(redirect string) bool {
	for _, candidate := range c.RedirectURIs {
		if candidate == redirect {
			return true
		}
	}
	return false
}
