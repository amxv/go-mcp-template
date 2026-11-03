package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"time"
)

const sessionCookie = "mcp_oauth_session"

type authorizationRequest struct {
	ClientID    string `json:"client_id"`
	ClientName  string `json:"client_name"`
	RedirectURI string `json:"redirect_uri"`
	Resource    string `json:"resource"`
	State       string `json:"state"`
	Challenge   string `json:"challenge"`
	Expires     int64  `json:"expires"`
}

type browserSession struct {
	Authenticated bool   `json:"authenticated"`
	PendingID     string `json:"pending_id"`
	CSRF          string `json:"csrf"`
	Expires       int64  `json:"expires"`
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	p, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !singleValues(p) {
		oauthError(w, 400, "invalid_request")
		return
	}
	client, err := s.resolveClient(r.Context(), p.Get("client_id"))
	if err != nil {
		oauthError(w, 400, "invalid_client")
		return
	}
	if !client.Allows(p.Get("redirect_uri")) {
		oauthError(w, 400, "invalid_client")
		return
	}
	a := authorizationRequest{ClientID: client.ID, ClientName: client.Name, RedirectURI: p.Get("redirect_uri"), Resource: p.Get("resource"), State: p.Get("state"), Challenge: p.Get("code_challenge"), Expires: time.Now().Add(pendingTTL).Unix()}
	fail := func(code string) { http.Redirect(w, r, s.redirect(a, "error", code), http.StatusSeeOther) }
	if !bounded(a.State, 1024) || a.Resource != s.Resource() || p.Get("response_type") != "code" || p.Get("code_challenge_method") != "S256" || !validToken(a.Challenge) || (p.Get("scope") != "" && p.Get("scope") != Scope) || (p.Get("response_mode") != "" && p.Get("response_mode") != "query") {
		fail("invalid_request")
		return
	}
	for _, name := range []string{"client_secret", "request", "request_uri", "client_assertion", "client_assertion_type"} {
		if p.Has(name) {
			fail("invalid_request")
			return
		}
	}
	pendingID, err := randomToken()
	if err != nil {
		http.Error(w, "Authentication unavailable", 503)
		return
	}
	raw, _ := json.Marshal(a)
	if err := s.store.Put(r.Context(), "pending", pendingID, raw, pendingTTL); err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	// The actual user's browser may differ from ChatGPT's authorization preflight.
	// Render sign-in directly at the validated authorize URL, preserving its params.
	current, _, _ := s.readSession(r)
	_, session, err := s.newSession(w, r, current.Authenticated, pendingID)
	if err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	if session.Authenticated {
		http.Redirect(w, r, "/oauth/consent", http.StatusSeeOther)
		return
	}
	s.page(w, pageData{Kind: "login", CSRF: session.CSRF, ForClient: client.Name})
}

func (s *Server) redirect(a authorizationRequest, kind, value string) string {
	// This is only called after an allowlisted client's exact URI is checked.
	if !validRedirect(a.RedirectURI) {
		return s.config.Issuer + "/oauth/login"
	}
	u, _ := url.Parse(a.RedirectURI)
	q := u.Query()
	q.Set(kind, value)
	q.Set("iss", s.config.Issuer)
	q.Set("state", a.State)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Server) readSession(r *http.Request) (browserSession, string, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || !validToken(cookie.Value) {
		return browserSession{}, "", ErrMissing
	}
	raw, err := s.store.Get(r.Context(), "session", cookie.Value)
	if err != nil {
		return browserSession{}, "", err
	}
	var session browserSession
	if strictJSON(raw, &session) != nil || !validToken(session.CSRF) || session.Expires <= time.Now().Unix() || (session.PendingID != "" && !validToken(session.PendingID)) {
		return browserSession{}, "", ErrMissing
	}
	return session, cookie.Value, nil
}

func (s *Server) newSession(w http.ResponseWriter, r *http.Request, authenticated bool, pending string) (string, browserSession, error) {
	id, err := randomToken()
	if err != nil {
		return "", browserSession{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", browserSession{}, err
	}
	session := browserSession{Authenticated: authenticated, PendingID: pending, CSRF: csrf, Expires: time.Now().Add(sessionTTL).Unix()}
	raw, _ := json.Marshal(session)
	if err := s.store.Put(r.Context(), "session", id, raw, sessionTTL); err != nil {
		return "", browserSession{}, err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/oauth", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds())})
	return id, session, nil
}

func (s *Server) browser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if r.Method == http.MethodPost && r.Header.Get("Origin") != s.config.Issuer {
		http.Error(w, "Invalid browser origin", 403)
		return
	}
	switch r.URL.Path {
	case "/oauth/login":
		if r.Method == http.MethodGet {
			s.loginPage(w, r)
		} else {
			s.login(w, r)
		}
	case "/oauth/consent":
		if r.Method == http.MethodGet {
			s.consentPage(w, r)
		} else {
			s.consent(w, r)
		}
	case "/oauth/grants":
		if !getOnly(w, r) {
			return
		}
		s.grantsPage(w, r)
	case "/oauth/revoke":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}
		s.revoke(w, r)
	case "/oauth/logout":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}
		s.logout(w, r)
	}
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	session, _, err := s.readSession(r)
	if errors.Is(err, ErrMissing) {
		_, session, err = s.newSession(w, r, false, "")
	}
	if err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	if session.Authenticated {
		http.Redirect(w, r, "/oauth/grants", http.StatusSeeOther)
		return
	}
	s.page(w, pageData{Kind: "login", CSRF: session.CSRF})
}

func browserForm(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	return parseForm(w, r)
}

func sameCSRF(a, b string) bool {
	return validToken(a) && validToken(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) checkedSession(w http.ResponseWriter, r *http.Request, p url.Values) (browserSession, string, bool) {
	session, id, err := s.readSession(r)
	if errors.Is(err, ErrMissing) {
		http.Error(w, "Session expired. Start sign-in again.", 401)
		return browserSession{}, "", false
	}
	if err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return browserSession{}, "", false
	}
	if !sameCSRF(session.CSRF, p.Get("csrf")) {
		http.Error(w, "Invalid CSRF token", 403)
		return browserSession{}, "", false
	}
	return session, id, true
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	p, ok := browserForm(w, r)
	if !ok {
		return
	}
	session, id, ok := s.checkedSession(w, r, p)
	if !ok {
		return
	}
	allowed, err := s.store.AllowLogin(r.Context())
	if err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many login attempts", 429)
		return
	}
	supplied := sha256.Sum256([]byte(p.Get("secret")))
	wanted := sha256.Sum256([]byte(s.config.OwnerSecret))
	if subtle.ConstantTimeCompare(supplied[:], wanted[:]) != 1 || session.Authenticated {
		s.pageStatus(w, 401, pageData{Kind: "login", CSRF: session.CSRF, Error: "Sign-in unsuccessful."})
		return
	}
	if _, _, err := s.newSession(w, r, true, session.PendingID); err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	_ = s.store.Delete(r.Context(), "session", id)
	if session.PendingID != "" {
		http.Redirect(w, r, "/oauth/consent", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/oauth/grants", http.StatusSeeOther)
	}
}

func (s *Server) pending(r *http.Request, session browserSession) (authorizationRequest, error) {
	if !validToken(session.PendingID) {
		return authorizationRequest{}, ErrMissing
	}
	raw, err := s.store.Get(r.Context(), "pending", session.PendingID)
	if err != nil {
		return authorizationRequest{}, err
	}
	var a authorizationRequest
	if strictJSON(raw, &a) != nil || a.Expires <= time.Now().Unix() || a.Resource != s.Resource() || !validToken(a.Challenge) {
		return authorizationRequest{}, ErrMissing
	}
	client, ok := s.staticClient(a.ClientID)
	if !ok || !client.Allows(a.RedirectURI) {
		return authorizationRequest{}, ErrMissing
	}
	return a, nil
}

func (s *Server) consentPage(w http.ResponseWriter, r *http.Request) {
	session, _, err := s.readSession(r)
	if err != nil || !session.Authenticated {
		http.Error(w, "Sign in via the original authorization URL", 401)
		return
	}
	a, err := s.pending(r, session)
	if err != nil {
		tokenStateError(w, err)
		return
	}
	callback, _ := url.Parse(a.RedirectURI) // already checked by pending()
	s.page(w, pageData{Kind: "consent", CSRF: session.CSRF, ForClient: a.ClientName, Resource: a.Resource, CallbackOrigin: callback.Scheme + "://" + callback.Host})
}

func (s *Server) consent(w http.ResponseWriter, r *http.Request) {
	p, ok := browserForm(w, r)
	if !ok {
		return
	}
	session, id, ok := s.checkedSession(w, r, p)
	if !ok {
		return
	}
	if !session.Authenticated {
		http.Error(w, "Unauthenticated", 401)
		return
	}
	a, err := s.pending(r, session)
	if err != nil {
		tokenStateError(w, err)
		return
	}
	if p.Get("decision") != "approve" && p.Get("decision") != "deny" {
		http.Error(w, "Invalid consent", 400)
		return
	}
	// Atomic consume prevents a second browser POST from granting twice.
	if _, err = s.store.Take(r.Context(), "pending", session.PendingID); err != nil {
		tokenStateError(w, err)
		return
	}
	_ = s.store.Delete(r.Context(), "session", id)
	if p.Get("decision") == "deny" {
		http.Redirect(w, r, s.redirect(a, "error", "access_denied"), http.StatusSeeOther)
		return
	}
	grantID, err := randomToken()
	if err != nil {
		http.Error(w, "Authentication unavailable", 503)
		return
	}
	code, err := randomToken()
	if err != nil {
		http.Error(w, "Authentication unavailable", 503)
		return
	}
	grant := Grant{ID: grantID, ClientID: a.ClientID, ClientName: a.ClientName, Resource: s.Resource(), Scope: Scope, Expires: time.Now().Add(grantTTL).Unix()}
	if err := s.store.CreateGrant(r.Context(), grant); err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	record := AuthorizationCode{GrantID: grantID, ClientID: a.ClientID, RedirectURI: a.RedirectURI, Resource: a.Resource, Challenge: a.Challenge, Expires: time.Now().Add(codeTTL).Unix()}
	raw, _ := json.Marshal(record)
	if err := s.store.Put(r.Context(), "code", code, raw, codeTTL); err != nil {
		_ = s.store.RevokeGrant(r.Context(), grantID)
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	http.Redirect(w, r, s.redirect(a, "code", code), http.StatusSeeOther)
}

func (s *Server) grantsPage(w http.ResponseWriter, r *http.Request) {
	session, _, err := s.readSession(r)
	if err != nil || !session.Authenticated {
		http.Redirect(w, r, "/oauth/login", http.StatusSeeOther)
		return
	}
	grants, err := s.store.ListGrants(r.Context())
	if err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i].ClientName < grants[j].ClientName })
	s.page(w, pageData{Kind: "grants", CSRF: session.CSRF, Grants: grants})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	p, ok := browserForm(w, r)
	if !ok {
		return
	}
	session, _, ok := s.checkedSession(w, r, p)
	if !ok {
		return
	}
	if !session.Authenticated {
		http.Error(w, "Unauthenticated", 401)
		return
	}
	id := p.Get("grant_id")
	if !validToken(id) {
		http.Error(w, "Invalid grant", 400)
		return
	}
	if err := s.store.RevokeGrant(r.Context(), id); err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	http.Redirect(w, r, "/oauth/grants", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	p, ok := browserForm(w, r)
	if !ok {
		return
	}
	_, id, ok := s.checkedSession(w, r, p)
	if !ok {
		return
	}
	if err := s.store.Delete(r.Context(), "session", id); err != nil {
		http.Error(w, "Authentication state unavailable", 503)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/oauth", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, "/oauth/login", http.StatusSeeOther)
}

type pageData struct {
	Kind           string
	CSRF           string
	Error          string
	ForClient      string
	Resource       string
	Grants         []Grant
	CallbackOrigin string
}

func (s *Server) page(w http.ResponseWriter, data pageData) { s.pageStatus(w, 200, data) }

func (s *Server) pageStatus(w http.ResponseWriter, code int, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "same-origin")
	policy := "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	if data.Kind == "consent" && data.CallbackOrigin != "" {
		// Browser form-action can apply to the POST's final 303 redirect.
		// Permit only the previously validated client's exact callback origin.
		policy = "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' " + data.CallbackOrigin + "; frame-ancestors 'none'; base-uri 'none'"
	}
	w.Header().Set("Content-Security-Policy", policy)
	w.WriteHeader(code)
	_ = browserTemplate.Execute(w, data)
}

var browserTemplate = template.Must(template.New("oauth-page").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>MCP authorization</title><style>body{font-family:system-ui,sans-serif;background:#f8fafc;color:#17202b;margin:0;padding:24px}main{max-width:480px;margin:10vh auto;background:white;padding:32px;border:1px solid #e2e8f0;border-radius:16px;box-shadow:0 10px 28px #0000000a}h1{margin:0 0 14px;font-size:24px}p{line-height:1.55;color:#475569}input{display:block;width:100%;box-sizing:border-box;padding:12px;margin:12px 0 20px;border:1px solid #cbd5e1;border-radius:8px}button{background:#166534;color:#fff;border:0;border-radius:8px;padding:12px 16px;cursor:pointer;margin:4px 4px 4px 0}button.secondary{background:#475569}.grant{border-top:1px solid #e2e8f0;padding:14px 0}.error{color:#b91c1c}small{color:#64748b}</style></head><body><main>
{{if eq .Kind "login"}}<h1>Connect to your MCP server</h1>{{if .ForClient}}<p>Sign in to authorize <strong>{{.ForClient}}</strong>.</p>{{else}}<p>Sign in to manage connected clients.</p>{{end}}{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form action="/oauth/login" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><label for="secret">Owner secret</label><input id="secret" type="password" name="secret" autocomplete="off" required><button type="submit">Sign in</button></form><small>Use the secret configured in MCP_OAUTH_OWNER_SECRET. Never share it with a client.</small>
{{else if eq .Kind "consent"}}<h1>Allow connection?</h1><p><strong>{{.ForClient}}</strong> is requesting permission to access <strong>{{.Resource}}</strong>.</p><p>Scope: <code>mcp:access</code>. This permits the client to call all exposed MCP tools.</p><form action="/oauth/consent" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><button type="submit" name="decision" value="approve">Allow connection</button><button class="secondary" type="submit" name="decision" value="deny">Deny</button></form>
{{else if eq .Kind "grants"}}<h1>Connected clients</h1><p>Revoking a grant immediately invalidates its access and refresh tokens.</p>{{range .Grants}}<div class="grant"><strong>{{.ClientName}}</strong><p><small>{{.ClientID}}</small></p><form action="/oauth/revoke" method="post"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="grant_id" value="{{.ID}}"><button class="secondary" type="submit">Revoke</button></form></div>{{else}}<p>No active grants.</p>{{end}}<form action="/oauth/logout" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="secondary" type="submit">Sign out</button></form>{{end}}
</main></body></html>`))

// Allowlisted client redirection is always server-generated; never accept a
// return_to or callback field from a browser POST.
