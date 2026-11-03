package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMissing     = errors.New("OAuth record missing or expired")
	ErrConflict    = errors.New("OAuth record already exists")
	ErrUnavailable = errors.New("OAuth durable state unavailable")
)

// Store must be shared across all Vercel instances. Take and SwapRefresh must
// be atomic; otherwise codes and refresh tokens could be used more than once.
type Store interface {
	Put(context.Context, string, string, []byte, time.Duration) error
	Get(context.Context, string, string) ([]byte, error)
	Take(context.Context, string, string) ([]byte, error)
	Delete(context.Context, string, string) error
	CreateGrant(context.Context, Grant) error
	ListGrants(context.Context) ([]Grant, error)
	RevokeGrant(context.Context, string) error
	SwapRefresh(context.Context, string, string, []byte, []byte, string, time.Duration) (bool, error)
	AllowLogin(context.Context) (bool, error)
	AllowClientMetadata(context.Context) (bool, error)
}

type RedisStore struct {
	endpoint string
	token    string
	prefix   string
	client   *http.Client
}

func NewRedisStoreFromEnv(issuer string) (*RedisStore, error) {
	return NewRedisStore(os.Getenv("MCP_OAUTH_REDIS_REST_URL"), os.Getenv("MCP_OAUTH_REDIS_REST_TOKEN"), issuer)
}

func NewRedisStore(endpoint, token, issuer string) (*RedisStore, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || !validHost(strings.ToLower(u.Hostname())) || u.User != nil || u.Port() != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || len(token) < 8 || len(token) > 8192 || issuer == "" {
		return nil, errors.New("invalid HTTPS Redis REST URL or Redis token")
	}
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 16 << 10, MaxIdleConnsPerHost: 8, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != u.Hostname() || port != "443" {
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
	hash := sha256.Sum256([]byte(issuer))
	return &RedisStore{endpoint: strings.TrimRight(endpoint, "/"), token: token, prefix: "go-mcp:oauth:{" + hex.EncodeToString(hash[:]) + "}:v1:", client: &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}}, nil
}

func publicIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}

func (s *RedisStore) key(bucket, id string) string {
	hash := sha256.Sum256([]byte(id))
	return s.prefix + bucket + ":" + hex.EncodeToString(hash[:])
}

func (s *RedisStore) command(ctx context.Context, args ...any) (json.RawMessage, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	// Listing the bounded set of 128 grants can exceed the per-record limit.
	// Keep the response cap bounded without rejecting valid owner grant lists.
	data, err := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return nil, ErrUnavailable
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if json.Unmarshal(data, &out) != nil || out.Error != "" || len(out.Result) == 0 {
		return nil, ErrUnavailable
	}
	return out.Result, nil
}

func (s *RedisStore) Put(ctx context.Context, bucket, id string, data []byte, ttl time.Duration) error {
	if len(data) == 0 || len(data) > 16384 || !json.Valid(data) || id == "" || ttl < time.Second || ttl > 31*24*time.Hour || ttl%time.Second != 0 {
		return ErrUnavailable
	}
	result, err := s.command(ctx, "SET", s.key(bucket, id), string(data), "NX", "EX", int64(ttl/time.Second))
	if err != nil {
		return err
	}
	switch string(result) {
	case `"OK"`:
		return nil
	case "null":
		return ErrConflict
	default:
		return ErrUnavailable
	}
}

func parseValue(result json.RawMessage) ([]byte, error) {
	if string(result) == "null" || string(result) == "false" {
		return nil, ErrMissing
	}
	var value string
	if json.Unmarshal(result, &value) != nil || len(value) > 16384 || !json.Valid([]byte(value)) {
		return nil, ErrUnavailable
	}
	return []byte(value), nil
}

func (s *RedisStore) Get(ctx context.Context, bucket, id string) ([]byte, error) {
	result, err := s.command(ctx, "GET", s.key(bucket, id))
	if err != nil {
		return nil, err
	}
	return parseValue(result)
}

func (s *RedisStore) Take(ctx context.Context, bucket, id string) ([]byte, error) {
	result, err := s.command(ctx, "GETDEL", s.key(bucket, id))
	if err != nil {
		return nil, err
	}
	return parseValue(result)
}

func (s *RedisStore) Delete(ctx context.Context, bucket, id string) error {
	result, err := s.command(ctx, "DEL", s.key(bucket, id))
	if err != nil {
		return err
	}
	if string(result) != "0" && string(result) != "1" {
		return ErrUnavailable
	}
	return nil
}

const createGrantScript = `if redis.call('EXISTS',KEYS[1])==1 then return 0 end; local records=redis.call('HGETALL',KEYS[2]); local count=0; for i=1,#records,2 do local grant=cjson.decode(records[i+1]); if grant.expires<=tonumber(ARGV[4]) then redis.call('HDEL',KEYS[2],records[i]) else count=count+1 end end; if count>=128 then return -1 end; redis.call('SET',KEYS[1],ARGV[1],'EX',ARGV[2]); redis.call('HSET',KEYS[2],ARGV[3],ARGV[1]); redis.call('EXPIRE',KEYS[2],ARGV[2]); return 1`

func (s *RedisStore) CreateGrant(ctx context.Context, grant Grant) error {
	raw, err := json.Marshal(grant)
	if err != nil || len(raw) > 4096 {
		return ErrUnavailable
	}
	result, err := s.command(ctx, "EVAL", createGrantScript, 2, s.key("grant", grant.ID), s.prefix+"grant-index", string(raw), int64(grantTTL/time.Second), grant.ID, time.Now().Unix())
	if err != nil {
		return err
	}
	if string(result) == "1" {
		return nil
	}
	if string(result) == "0" || string(result) == "-1" {
		return ErrConflict
	}
	return ErrUnavailable
}

func (s *RedisStore) ListGrants(ctx context.Context) ([]Grant, error) {
	result, err := s.command(ctx, "HVALS", s.prefix+"grant-index")
	if err != nil {
		return nil, err
	}
	var values []string
	if json.Unmarshal(result, &values) != nil || len(values) > 128 {
		return nil, ErrUnavailable
	}
	var grants []Grant
	for _, value := range values {
		var grant Grant
		if json.Unmarshal([]byte(value), &grant) != nil || !validToken(grant.ID) {
			return nil, ErrUnavailable
		}
		if grant.Expires > time.Now().Unix() {
			if _, err := s.Get(ctx, "grant", grant.ID); err == nil {
				grants = append(grants, grant)
			} else if !errors.Is(err, ErrMissing) {
				return nil, err
			}
		}
	}
	return grants, nil
}

const revokeScript = `redis.call('DEL',KEYS[1]); redis.call('HDEL',KEYS[2],ARGV[1]); return 1`

func (s *RedisStore) RevokeGrant(ctx context.Context, id string) error {
	if !validToken(id) {
		return ErrMissing
	}
	result, err := s.command(ctx, "EVAL", revokeScript, 2, s.key("grant", id), s.prefix+"grant-index", id)
	if err != nil || string(result) != "1" {
		return ErrUnavailable
	}
	return nil
}

// The replay tombstone allows a second use of a rotated token to revoke its
// grant. All keys share a Redis Cluster hash tag and are changed atomically.
const rotateScript = `local old=redis.call('GET',KEYS[1]); if not old or old~=ARGV[1] then return 0 end; if redis.call('EXISTS',KEYS[2])==1 then return 0 end; redis.call('SET',KEYS[2],ARGV[2],'EX',ARGV[3]); redis.call('SET',KEYS[3],ARGV[4],'EX',ARGV[3]); redis.call('DEL',KEYS[1]); return 1`

func (s *RedisStore) SwapRefresh(ctx context.Context, oldID, newID string, original, replacement []byte, grantID string, ttl time.Duration) (bool, error) {
	if oldID == newID || len(original) == 0 || len(replacement) == 0 || !json.Valid(original) || !json.Valid(replacement) || ttl < time.Second {
		return false, ErrUnavailable
	}
	replay, _ := json.Marshal(grantID)
	result, err := s.command(ctx, "EVAL", rotateScript, 3, s.key("refresh", oldID), s.key("refresh", newID), s.key("replay", oldID), string(original), string(replacement), int64(ttl/time.Second), string(replay))
	if err != nil {
		return false, err
	}
	if string(result) != "0" && string(result) != "1" {
		return false, ErrUnavailable
	}
	return string(result) == "1", nil
}

const loginRateScript = `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],60) end; if n>20 then return 0 else return 1 end`

func (s *RedisStore) AllowLogin(ctx context.Context) (bool, error) {
	return s.allowRate(ctx, "login-rate", loginRateScript)
}

const clientMetadataRateScript = `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],60) end; if n>12 then return 0 else return 1 end`

func (s *RedisStore) AllowClientMetadata(ctx context.Context) (bool, error) {
	return s.allowRate(ctx, "client-metadata-rate", clientMetadataRateScript)
}

func (s *RedisStore) allowRate(ctx context.Context, bucket, script string) (bool, error) {
	result, err := s.command(ctx, "EVAL", script, 1, s.prefix+bucket+":"+strconv.FormatInt(time.Now().Unix()/60, 10))
	if err != nil {
		return false, err
	}
	if string(result) != "0" && string(result) != "1" {
		return false, ErrUnavailable
	}
	return string(result) == "1", nil
}
