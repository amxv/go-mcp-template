package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const testIssuer = "https://api.example.com"
const testSecret = "sufficiently-long-random-test-owner-secret-1234567890"
const testClient = "test-client"
const testRedirect = "https://client.example.com/callback"

type item struct {
	data  []byte
	until time.Time
}
type memoryStore struct {
	sync.Mutex
	items  map[string]item
	grants map[string]Grant
	broken bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{items: map[string]item{}, grants: map[string]Grant{}}
}
func (m *memoryStore) check() error {
	if m.broken {
		return ErrUnavailable
	}
	return nil
}
func (m *memoryStore) key(bucket, id string) string { return bucket + ":" + id }
func (m *memoryStore) put(bucket, id string, v []byte, ttl time.Duration) error {
	key := m.key(bucket, id)
	if _, ok := m.items[key]; ok && time.Now().Before(m.items[key].until) {
		return ErrConflict
	}
	m.items[key] = item{data: bytes.Clone(v), until: time.Now().Add(ttl)}
	return nil
}
func (m *memoryStore) Put(_ context.Context, bucket, id string, v []byte, ttl time.Duration) error {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	return m.put(bucket, id, v, ttl)
}
func (m *memoryStore) get(bucket, id string) ([]byte, error) {
	v, ok := m.items[m.key(bucket, id)]
	if !ok || !time.Now().Before(v.until) {
		return nil, ErrMissing
	}
	return bytes.Clone(v.data), nil
}
func (m *memoryStore) Get(_ context.Context, bucket, id string) ([]byte, error) {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	return m.get(bucket, id)
}
func (m *memoryStore) Take(_ context.Context, bucket, id string) ([]byte, error) {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	v, err := m.get(bucket, id)
	if err == nil {
		delete(m.items, m.key(bucket, id))
	}
	return v, err
}
func (m *memoryStore) Delete(_ context.Context, bucket, id string) error {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	delete(m.items, m.key(bucket, id))
	return nil
}
func (m *memoryStore) CreateGrant(_ context.Context, g Grant) error {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	raw, _ := json.Marshal(g)
	if err := m.put("grant", g.ID, raw, time.Until(time.Unix(g.Expires, 0))); err != nil {
		return err
	}
	m.grants[g.ID] = g
	return nil
}
func (m *memoryStore) ListGrants(_ context.Context) ([]Grant, error) {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	var out []Grant
	for id, g := range m.grants {
		if _, err := m.get("grant", id); err == nil {
			out = append(out, g)
		}
	}
	return out, nil
}
func (m *memoryStore) RevokeGrant(_ context.Context, id string) error {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	delete(m.items, m.key("grant", id))
	delete(m.grants, id)
	return nil
}
func (m *memoryStore) SwapRefresh(_ context.Context, oldID, newID string, original, replacement []byte, grantID string, ttl time.Duration) (bool, error) {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return false, err
	}
	raw, err := m.get("refresh", oldID)
	if err != nil || !bytes.Equal(raw, original) {
		return false, nil
	}
	if err := m.put("refresh", newID, replacement, ttl); err != nil {
		return false, err
	}
	replay, _ := json.Marshal(grantID)
	_ = m.put("replay", oldID, replay, ttl)
	delete(m.items, m.key("refresh", oldID))
	return true, nil
}
func (m *memoryStore) AllowLogin(context.Context) (bool, error) {
	m.Lock()
	defer m.Unlock()
	if err := m.check(); err != nil {
		return false, err
	}
	return true, nil
}

func (m *memoryStore) AllowClientMetadata(context.Context) (bool, error) { return true, nil }

func fixture(t *testing.T) (*Server, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	server, err := New(Config{Issuer: testIssuer, OwnerSecret: testSecret, Clients: []Client{{ID: testClient, Name: "Testing Client", RedirectURIs: []string{testRedirect}}}}, store)
	if err != nil {
		t.Fatal(err)
	}
	return server, store
}

func request(t *testing.T, h http.Handler, method, path, body, origin, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, testIssuer+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

var csrfPattern = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func csrfFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	m := csrfPattern.FindStringSubmatch(w.Body.String())
	if len(m) != 2 {
		t.Fatalf("missing CSRF in page: %s", w.Body.String())
	}
	return m[1]
}

func cookieFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.Value != "" {
			return cookie.Name + "=" + cookie.Value
		}
	}
	t.Fatalf("missing session cookie: %s", w.Header())
	return ""
}

func authParams(client, redirect, challenge string) url.Values {
	return url.Values{"client_id": {client}, "redirect_uri": {redirect}, "response_type": {"code"}, "resource": {testIssuer + ResourcePath}, "scope": {Scope}, "state": {"some-client-state"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
}

func post(t *testing.T, h http.Handler, path string, p url.Values, cookie string) *httptest.ResponseRecorder {
	return request(t, h, "POST", path, p.Encode(), testIssuer, cookie)
}

func completeAuthorization(t *testing.T, h http.Handler) (string, string, string) {
	t.Helper()
	verifier := strings.Repeat("v", 64)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	w := request(t, h, "GET", "/oauth/authorize?"+authParams(testClient, testRedirect, challenge).Encode(), "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Testing Client") {
		t.Fatalf("authorize: %d %s", w.Code, w.Body.String())
	}
	cookie := cookieFrom(t, w)
	w = post(t, h, "/oauth/login", url.Values{"csrf": {csrfFrom(t, w)}, "secret": {testSecret}}, cookie)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/oauth/consent" {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	cookie = cookieFrom(t, w)
	w = request(t, h, "GET", "/oauth/consent", "", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "mcp:access") {
		t.Fatalf("consent page: %d %s", w.Code, w.Body.String())
	}
	w = post(t, h, "/oauth/consent", url.Values{"csrf": {csrfFrom(t, w)}, "decision": {"approve"}}, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("consent: %d %s", w.Code, w.Body.String())
	}
	callback, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if callback.Scheme+"://"+callback.Host+callback.Path != testRedirect || callback.Query().Get("state") != "some-client-state" || callback.Query().Get("iss") != testIssuer {
		t.Fatalf("callback tampered: %s", callback)
	}
	code := callback.Query().Get("code")
	if !validToken(code) {
		t.Fatal("invalid authorization code")
	}
	return code, verifier, cookie
}

func exchange(t *testing.T, h http.Handler, code, verifier string) *httptest.ResponseRecorder {
	return post(t, h, "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {testClient}, "redirect_uri": {testRedirect}, "resource": {testIssuer + ResourcePath}, "code": {code}, "code_verifier": {verifier}}, "")
}

func TestFullOAuthFlowRefreshReplayAndMCP(t *testing.T) {
	s, store := fixture(t)
	calls := 0
	h := s.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) }))
	w := request(t, h, "POST", "/mcp", "", "", "")
	if w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), testIssuer+ResourceMetadata) {
		t.Fatalf("challenge missing: %d %s", w.Code, w.Header())
	}
	for path, want := range map[string]string{ResourceMetadata: `"resource":"` + testIssuer + `/mcp"`, ResourceMetadata + ResourcePath: `"authorization_servers"`, AuthorizationMeta: `"client_id_metadata_document_supported":true`} {
		w = request(t, h, "GET", path, "", "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("metadata %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	code, verifier, _ := completeAuthorization(t, h)
	if w := exchange(t, h, code, strings.Repeat("x", 64)); w.Code != 400 {
		t.Fatalf("wrong PKCE used code: %d", w.Code)
	}
	w = exchange(t, h, code, verifier)
	if w.Code != 200 {
		t.Fatalf("code exchange: %d %s", w.Code, w.Body.String())
	}
	var tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Scope   string `json:"scope"`
		Expires int    `json:"expires_in"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	if !validToken(tokens.Access) || !validToken(tokens.Refresh) || tokens.Scope != Scope || tokens.Expires < 1 || tokens.Expires > 300 {
		t.Fatalf("invalid tokens: %s", w.Body.String())
	}
	if w := exchange(t, h, code, verifier); w.Code != 400 {
		t.Fatalf("reused code accepted: %d", w.Code)
	}
	protected := func(token, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", testIssuer+path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := protected("", "/mcp?key="+testSecret); w.Code != 401 {
		t.Fatalf("query key bypassed bearer: %d", w.Code)
	}
	if w := protected(tokens.Access, "/mcp"); w.Code != 204 || calls != 1 {
		t.Fatalf("valid bearer blocked: %d calls=%d", w.Code, calls)
	}
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {testClient}, "resource": {testIssuer + ResourcePath}, "refresh_token": {tokens.Refresh}}
	w = post(t, h, "/oauth/token", refresh, "")
	if w.Code != 200 {
		t.Fatalf("refresh: %d %s", w.Code, w.Body.String())
	}
	var next struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.Access == tokens.Access || next.Refresh == tokens.Refresh {
		t.Fatal("tokens did not rotate")
	}
	if w := protected(next.Access, "/mcp"); w.Code != 204 {
		t.Fatalf("refreshed access: %d", w.Code)
	}
	w = post(t, h, "/oauth/token", refresh, "")
	if w.Code != 400 {
		t.Fatalf("refresh replay: %d", w.Code)
	}
	if w := protected(next.Access, "/mcp"); w.Code != 401 {
		t.Fatalf("replay did not revoke grant: %d", w.Code)
	}
	store.Lock()
	if len(store.grants) != 0 {
		t.Fatal("grant not revoked")
	}
	store.Unlock()
}

func TestOAuthValidationAndBrowserSecurity(t *testing.T) {
	s, store := fixture(t)
	h := s.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	verifier := strings.Repeat("x", 64)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	p := authParams(testClient, testRedirect, challenge)
	for _, field := range []string{"resource", "redirect_uri", "code_challenge_method", "response_type"} {
		invalid := authParams(testClient, testRedirect, challenge)
		invalid.Set(field, "invalid")
		w := request(t, h, "GET", "/oauth/authorize?"+invalid.Encode(), "", "", "")
		if w.Code != 400 && w.Code != http.StatusSeeOther {
			t.Fatalf("invalid %s accepted: %d", field, w.Code)
		}
	}
	if w := request(t, h, "GET", "/oauth/authorize?"+p.Encode()+"&client_id=another", "", "", ""); w.Code != 400 {
		t.Fatalf("duplicate params: %d", w.Code)
	}
	r := httptest.NewRequest("GET", testIssuer+AuthorizationMeta, nil)
	r.Host = "attacker.example.com"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("host spoof: %d", w.Code)
	}
	r = httptest.NewRequest("GET", testIssuer+AuthorizationMeta, nil)
	r.Header.Set("X-Forwarded-Proto", "http")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("proxy spoof: %d", w.Code)
	}
	w = request(t, h, "GET", "/oauth/authorize?"+p.Encode(), "", "", "")
	cookie := cookieFrom(t, w)
	csrf := csrfFrom(t, w)
	if w := request(t, h, "POST", "/oauth/login", url.Values{"csrf": {csrf}, "secret": {testSecret}}.Encode(), "https://attacker.example.com", cookie); w.Code != 403 {
		t.Fatalf("CSRF origin: %d", w.Code)
	}
	if w := post(t, h, "/oauth/login", url.Values{"csrf": {strings.Repeat("x", 43)}, "secret": {testSecret}}, cookie); w.Code != 403 {
		t.Fatalf("CSRF value: %d", w.Code)
	}
	if w := post(t, h, "/oauth/login", url.Values{"csrf": {csrf}, "secret": {"wrong"}}, cookie); w.Code != 401 {
		t.Fatalf("wrong owner secret: %d", w.Code)
	}
	store.Lock()
	store.broken = true
	store.Unlock()
	if w := request(t, h, "POST", "/mcp", "", "", ""); w.Code != 401 {
		t.Fatalf("missing bearer bypass: %d", w.Code)
	}
	r = httptest.NewRequest("POST", testIssuer+"/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(hash[:]))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("Redis outage not fail-closed: %d", w.Code)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChatGPTClientMetadataIsAllowlisted(t *testing.T) {
	chatGPTCache.Lock()
	chatGPTCache.validUntil = time.Time{}
	chatGPTCache.Unlock()
	requests := 0
	c := Config{Issuer: testIssuer, OwnerSecret: testSecret, HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != ChatGPTClientID {
			t.Fatalf("SSRF to %s", r.URL)
		}
		body := `{"client_id":"` + ChatGPTClientID + `","client_name":"ChatGPT","redirect_uris":["` + ChatGPTRedirect + `"],"response_types":["code"],"grant_types":["authorization_code","refresh_token"],"token_endpoint_auth_methods_supported":["none","private_key_jwt"],"token_endpoint_auth_method":"private_key_jwt"}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	s, err := New(c, newMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	client, err := s.resolveClient(t.Context(), ChatGPTClientID)
	if err != nil || client.ID != ChatGPTClientID {
		t.Fatalf("ChatGPT metadata: %v", err)
	}
	if _, err := s.resolveClient(t.Context(), ChatGPTClientID); err != nil || requests != 1 {
		t.Fatalf("missing metadata cache: req=%d err=%v", requests, err)
	}
	if _, err := s.resolveClient(t.Context(), "https://evil.example/client.json"); err == nil {
		t.Fatal("arbitrary client URL accepted")
	}
	if strictJSON([]byte(`{"client_id":"a","CLIENT_ID":"b"}`), &Client{}) == nil {
		t.Fatal("ambiguous metadata accepted")
	}
}

func TestConfigIsStrict(t *testing.T) {
	cases := []Config{
		{Issuer: "http://api.example.com", OwnerSecret: testSecret},
		{Issuer: "https://api.example.com/", OwnerSecret: testSecret},
		{Issuer: "https://api.example.com:444", OwnerSecret: testSecret},
		{Issuer: testIssuer, OwnerSecret: "short"},
		{Issuer: testIssuer, OwnerSecret: testSecret, Clients: []Client{{ID: "x", Name: "x", RedirectURIs: []string{"http://attacker.example"}}}},
		{Issuer: testIssuer, OwnerSecret: testSecret, Clients: []Client{{ID: ChatGPTClientID, Name: "duplicate", RedirectURIs: []string{ChatGPTRedirect}}}},
	}
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Fatalf("invalid config %d accepted", i)
		}
	}
	if err := (Config{Issuer: testIssuer, OwnerSecret: testSecret}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerGrantRevocationDeniesExistingAccess(t *testing.T) {
	s, _ := fixture(t)
	h := s.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	code, verifier, _ := completeAuthorization(t, h)
	w := exchange(t, h, code, verifier)
	if w.Code != 200 {
		t.Fatalf("token exchange: %d", w.Code)
	}
	var tokens struct {
		Access string `json:"access_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", "/oauth/login", "", "", "")
	cookie := cookieFrom(t, w)
	w = post(t, h, "/oauth/login", url.Values{"csrf": {csrfFrom(t, w)}, "secret": {testSecret}}, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("owner sign-in: %d", w.Code)
	}
	cookie = cookieFrom(t, w)
	w = request(t, h, "GET", "/oauth/grants", "", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Testing Client") {
		t.Fatalf("grants listing: %d %s", w.Code, w.Body.String())
	}
	match := regexp.MustCompile(`name="grant_id" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("grant not listed")
	}
	w = post(t, h, "/oauth/revoke", url.Values{"csrf": {csrfFrom(t, w)}, "grant_id": {match[1]}}, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("POST", testIssuer+"/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+tokens.Access)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("revoked grant still usable: %d", w.Code)
	}
}

func TestRejectMalformedAccessHeaders(t *testing.T) {
	s, _ := fixture(t)
	h := s.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unverified access reached handler") }))
	for _, header := range []string{"Bearer x", "Basic x", "Bearer\t" + strings.Repeat("a", 43), "Bearer " + strings.Repeat("!", 43)} {
		r := httptest.NewRequest("POST", testIssuer+"/mcp", nil)
		r.Header.Add("Authorization", header)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("malformed header accepted: %q = %d", header, w.Code)
		}
	}
	// A second bearer header cannot be coalesced into a valid credential.
	r := httptest.NewRequest("POST", testIssuer+"/mcp", nil)
	r.Header.Add("Authorization", "Bearer x")
	r.Header.Add("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("duplicate header accepted")
	}
}
