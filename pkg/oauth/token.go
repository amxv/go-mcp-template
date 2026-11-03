package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Grant struct {
	ID         string `json:"id"`
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
	Resource   string `json:"resource"`
	Scope      string `json:"scope"`
	Expires    int64  `json:"expires"`
}

type AuthorizationCode struct {
	GrantID     string `json:"grant_id"`
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri"`
	Resource    string `json:"resource"`
	Challenge   string `json:"challenge"`
	Expires     int64  `json:"expires"`
}

type tokenRecord struct {
	GrantID  string `json:"grant_id"`
	ClientID string `json:"client_id"`
	Resource string `json:"resource"`
	Scope    string `json:"scope"`
	Expires  int64  `json:"expires"`
}

func (s *Server) grant(ctx context.Context, id, clientID string) (Grant, error) {
	if !validToken(id) {
		return Grant{}, ErrMissing
	}
	raw, err := s.store.Get(ctx, "grant", id)
	if err != nil {
		return Grant{}, err
	}
	var grant Grant
	if strictJSON(raw, &grant) != nil || grant.ID != id || grant.ClientID != clientID || grant.Resource != s.Resource() || grant.Scope != Scope {
		return Grant{}, ErrMissing
	}
	if _, ok := s.staticClient(clientID); !ok || grant.Expires <= time.Now().Unix() {
		return Grant{}, ErrMissing
	}
	return grant, nil
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	p, ok := parseForm(w, r)
	if !ok {
		return
	}
	for _, name := range []string{"client_secret", "client_assertion", "client_assertion_type", "password", "username"} {
		if p.Has(name) {
			oauthError(w, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	if p.Get("resource") != s.Resource() {
		oauthError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	client, valid := s.staticClient(p.Get("client_id"))
	if !valid {
		oauthError(w, http.StatusBadRequest, "invalid_client")
		return
	}
	switch p.Get("grant_type") {
	case "authorization_code":
		s.exchangeCode(w, r, p, client)
	case "refresh_token":
		s.exchangeRefresh(w, r, p, client)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func (s *Server) exchangeCode(w http.ResponseWriter, r *http.Request, p url.Values, client Client) {
	if !validToken(p.Get("code")) || !validVerifier(p.Get("code_verifier")) || !client.Allows(p.Get("redirect_uri")) || p.Has("refresh_token") || p.Has("scope") {
		oauthError(w, 400, "invalid_grant")
		return
	}
	raw, err := s.store.Get(r.Context(), "code", p.Get("code"))
	if err != nil {
		tokenStateError(w, err)
		return
	}
	var record AuthorizationCode
	if strictJSON(raw, &record) != nil || record.ClientID != client.ID || record.RedirectURI != p.Get("redirect_uri") || record.Resource != s.Resource() || record.Expires <= time.Now().Unix() || !checkPKCE(p.Get("code_verifier"), record.Challenge) {
		oauthError(w, 400, "invalid_grant")
		return
	}
	grant, err := s.grant(r.Context(), record.GrantID, client.ID)
	if err != nil {
		tokenStateError(w, err)
		return
	}
	consumed, err := s.store.Take(r.Context(), "code", p.Get("code"))
	if err != nil {
		tokenStateError(w, err)
		return
	}
	if !bytes.Equal(consumed, raw) || record.Expires <= time.Now().Unix() {
		oauthError(w, 400, "invalid_grant")
		return
	}
	s.issuePair(w, r.Context(), grant)
}

func (s *Server) exchangeRefresh(w http.ResponseWriter, r *http.Request, p url.Values, client Client) {
	if !validToken(p.Get("refresh_token")) || p.Has("code") || p.Has("code_verifier") || p.Has("redirect_uri") || (p.Has("scope") && p.Get("scope") != Scope) {
		oauthError(w, 400, "invalid_grant")
		return
	}
	oldID := p.Get("refresh_token")
	raw, err := s.store.Get(r.Context(), "refresh", oldID)
	if errors.Is(err, ErrMissing) {
		// Reusing a previously rotated token revokes the affected grant.
		if replay, e := s.store.Get(r.Context(), "replay", oldID); e == nil {
			var grantID string
			if json.Unmarshal(replay, &grantID) == nil && validToken(grantID) {
				if e = s.store.RevokeGrant(r.Context(), grantID); e != nil {
					tokenStateError(w, e)
					return
				}
			}
		} else if !errors.Is(e, ErrMissing) {
			tokenStateError(w, e)
			return
		}
		// Never disclose whether a refresh token was replayed.
		oauthError(w, 400, "invalid_grant")
		return
	}
	if err != nil {
		tokenStateError(w, err)
		return
	}
	var previous tokenRecord
	if strictJSON(raw, &previous) != nil || previous.ClientID != client.ID || previous.Resource != s.Resource() || previous.Scope != Scope || previous.Expires <= time.Now().Unix() {
		oauthError(w, 400, "invalid_grant")
		return
	}
	grant, err := s.grant(r.Context(), previous.GrantID, client.ID)
	if err != nil {
		tokenStateError(w, err)
		return
	}
	newRefresh, err := randomToken()
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	newAccess, err := randomToken()
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	now := time.Now()
	exp := min(now.Add(accessTTL).Unix(), grant.Expires)
	access := tokenRecord{GrantID: grant.ID, ClientID: client.ID, Resource: s.Resource(), Scope: Scope, Expires: exp}
	accessRaw, _ := json.Marshal(access)
	if err := s.store.Put(r.Context(), "access", newAccess, accessRaw, time.Duration(exp-now.Unix())*time.Second); err != nil {
		tokenStateError(w, err)
		return
	}
	nextRaw, _ := json.Marshal(previous)
	rotated, err := s.store.SwapRefresh(r.Context(), oldID, newRefresh, raw, nextRaw, grant.ID, time.Duration(grant.Expires-now.Unix())*time.Second)
	if err != nil || !rotated {
		_ = s.store.Delete(r.Context(), "access", newAccess)
		if err != nil {
			tokenStateError(w, err)
		} else {
			oauthError(w, 400, "invalid_grant")
		}
		return
	}
	s.tokenResponse(w, newAccess, newRefresh, exp-now.Unix())
}

func (s *Server) issuePair(w http.ResponseWriter, ctx context.Context, grant Grant) {
	access, err := randomToken()
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	refresh, err := randomToken()
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	now := time.Now()
	exp := min(now.Add(accessTTL).Unix(), grant.Expires)
	record := tokenRecord{GrantID: grant.ID, ClientID: grant.ClientID, Resource: grant.Resource, Scope: grant.Scope, Expires: exp}
	raw, _ := json.Marshal(record)
	if err := s.store.Put(ctx, "access", access, raw, time.Duration(exp-now.Unix())*time.Second); err != nil {
		tokenStateError(w, err)
		return
	}
	record.Expires = grant.Expires
	raw, _ = json.Marshal(record)
	if err := s.store.Put(ctx, "refresh", refresh, raw, time.Duration(grant.Expires-now.Unix())*time.Second); err != nil {
		_ = s.store.Delete(ctx, "access", access)
		tokenStateError(w, err)
		return
	}
	s.tokenResponse(w, access, refresh, exp-now.Unix())
}

func (s *Server) tokenResponse(w http.ResponseWriter, access, refresh string, expires int64) {
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": expires,
		"refresh_token": refresh, "scope": Scope,
	})
}

func tokenStateError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrMissing) || errors.Is(err, ErrConflict) {
		oauthError(w, 400, "invalid_grant")
	} else {
		oauthError(w, 503, "temporarily_unavailable")
	}
}

func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		challenge := `Bearer resource_metadata="` + s.config.Issuer + ResourceMetadata + `", scope="` + Scope + `"`
		deny := func(invalid bool) {
			value := challenge
			if invalid {
				value += `, error="invalid_token"`
			}
			w.Header().Set("WWW-Authenticate", value)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}
		if r.URL.Query().Has("access_token") {
			deny(true)
			return
		}
		headers := r.Header.Values("Authorization")
		if len(headers) == 0 {
			deny(false)
			return
		}
		if len(headers) != 1 {
			deny(true)
			return
		}
		scheme, token, found := strings.Cut(headers[0], " ")
		token = strings.TrimLeft(token, " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || !validToken(token) {
			deny(true)
			return
		}
		raw, err := s.store.Get(r.Context(), "access", token)
		if errors.Is(err, ErrMissing) {
			deny(true)
			return
		}
		if err != nil {
			http.Error(w, "Authentication state unavailable", 503)
			return
		}
		var record tokenRecord
		if strictJSON(raw, &record) != nil || record.Resource != s.Resource() || record.Scope != Scope || record.Expires <= time.Now().Unix() {
			deny(true)
			return
		}
		if _, err := s.grant(r.Context(), record.GrantID, record.ClientID); err != nil {
			if errors.Is(err, ErrUnavailable) {
				http.Error(w, "Authentication state unavailable", 503)
			} else {
				deny(true)
			}
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, Principal{Subject: "owner", ClientID: record.ClientID, Scope: Scope})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
