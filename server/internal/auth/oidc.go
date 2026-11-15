package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"kallisti.chat/server/internal/fedi"
	"kallisti.chat/server/internal/password"
	"kallisti.chat/server/internal/store"
)

const oidcPath = "/api/oidc"
const oidcStateCookie = "kallisti_oidc_state"
const oidcSignupCookie = "kallisti_oidc_signup"

// oidclogin is one administrator-configured provider. access and refresh tokens
// are never persisted or returned to the browser.
type OIDCLogin struct {
	issuer, name, origin string
	config               oauth2.Config
	client               *http.Client
	discoveryMu          sync.Mutex
	provider             *oidc.Provider
	mu                   sync.Mutex // identity changes, signup redemption, session creation and revocation
	flows                pending[oidcFlow]
	signups              pending[oidcSignup]
	proofs               map[string]pendingEntry[store.OIDCIdentity]
}

type oidcFlow struct {
	verifier, nonce, mode, session, passwordHash string
	account                                      int64
	started                                      time.Time
}
type oidcSignup struct {
	identity store.OIDCIdentity
	name     string
}

func secureOIDCURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("oidc urls require https, a host, and no user information or fragment")
	}
	return u, nil
}

type oidcTransport struct{ base http.RoundTripper }

func (t oidcTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if _, err := secureOIDCURL(r.URL.String()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(r)
}

// NewOIDCLogin: the settings come checked (see config.parseOIDC). Discovery
// is lazy, so a provider outage cannot keep the other logins from starting.
func NewOIDCLogin(issuer, clientID, clientSecret, redirectURL, name string) (*OIDCLogin, error) {
	return newOIDCLogin(issuer, clientID, clientSecret, redirectURL, name, http.DefaultTransport)
}

func newOIDCLogin(issuer, clientID, clientSecret, redirectURL, name string, transport http.RoundTripper) (*OIDCLogin, error) {
	redirect, err := url.Parse(redirectURL)
	if err != nil {
		return nil, err
	}
	return &OIDCLogin{issuer: issuer, name: name, origin: redirect.Scheme + "://" + redirect.Host,
		config: oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL, Scopes: []string{oidc.ScopeOpenID, "profile"}},
		client: &http.Client{Timeout: 10 * time.Second, Transport: oidcTransport{transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("oidc redirects are not allowed") }},
		proofs: make(map[string]pendingEntry[store.OIDCIdentity]),
	}, nil
}

func (o *OIDCLogin) discover(ctx context.Context) error {
	o.discoveryMu.Lock()
	defer o.discoveryMu.Unlock()
	if o.provider != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, o.client), 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, o.issuer)
	if err != nil {
		return errors.New("oidc discovery failed")
	}
	var metadata struct {
		JWKS string `json:"jwks_uri"`
	}
	if err = p.Claims(&metadata); err != nil {
		return errors.New("invalid oidc metadata")
	}
	for _, raw := range []string{p.Endpoint().AuthURL, p.Endpoint().TokenURL, metadata.JWKS} {
		if _, err = secureOIDCURL(raw); err != nil {
			return errors.New("insecure oidc endpoint")
		}
	}
	o.config.Endpoint, o.provider = p.Endpoint(), p
	return nil
}

func (o *OIDCLogin) home(result, reason string) string {
	q := url.Values{"oidc": {result}}
	if reason != "" {
		q.Set("reason", reason)
	}
	return o.origin + "/?" + q.Encode()
}

func (a *Service) setOIDCCookie(w http.ResponseWriter, r *http.Request, name, value string, life time.Duration) {
	if value == "" {
		a.clearCookie(w, r, name)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(r, name), Value: value, Path: "/", MaxAge: int(life.Seconds()), HttpOnly: true, Secure: a.secure(r), SameSite: http.SameSiteLaxMode})
}

func (a *Service) serveOIDC(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != oidcPath && !strings.HasPrefix(r.URL.Path, oidcPath+"/") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == oidcPath && r.Method == http.MethodGet {
		name := ""
		if a.OIDC != nil {
			name = a.OIDC.name
		}
		writeJSON(w, 200, map[string]any{"enabled": a.OIDC != nil, "name": name})
		return true
	}
	if a.OIDC == nil && (r.URL.Path != oidcPath+"/link" || r.Method != http.MethodDelete) {
		http.NotFound(w, r)
		return true
	}
	if r.Method != http.MethodGet && !a.ownSite(w, r, "use this server's own site for openid login") {
		return true
	}
	switch {
	case r.URL.Path == oidcPath+"/start" && r.Method == http.MethodPost:
		a.oidcStart(w, r)
	case r.URL.Path == oidcPath+"/callback" && r.Method == http.MethodGet:
		a.oidcCallback(w, r)
	case r.URL.Path == oidcPath+"/signup" && r.Method == http.MethodGet:
		s, ok := a.OIDC.signups.get(a.cookie(r, oidcSignupCookie), a.Now(), false)
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "the login expired, please try again"})
		} else {
			writeJSON(w, 200, map[string]string{"name": s.name, "provider": a.OIDC.name})
		}
	case r.URL.Path == oidcPath+"/signup" && r.Method == http.MethodPost:
		a.oidcSignup(w, r)
	case r.URL.Path == oidcPath+"/link" && r.Method == http.MethodDelete:
		a.oidcUnlink(w, r)
	default:
		http.NotFound(w, r)
	}
	return true
}

func (a *Service) oidcStart(w http.ResponseWriter, r *http.Request) {
	if !a.secure(r) {
		writeJSON(w, 403, map[string]string{"error": "openid login requires a secure connection"})
		return
	}
	if !a.allow(r) {
		writeJSON(w, 429, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var c struct {
		Mode     string `json:"mode"`
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if c.Mode == "" {
		c.Mode = "login"
	}
	if c.Mode != "login" && c.Mode != "link" && c.Mode != "reauth" {
		writeJSON(w, 400, map[string]string{"error": "invalid login mode"})
		return
	}
	flow := oidcFlow{mode: c.Mode, verifier: oauth2.GenerateVerifier(), nonce: randomHex(32), started: a.Now()}
	if c.Mode != "login" {
		acc, ok := a.AccountFor(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "not logged in"})
			return
		}
		if c.Mode == "link" {
			if acc.PasswordHash == "" && !acc.Temporary {
				writeJSON(w, 409, map[string]string{"error": "set a password before linking another login method"})
				return
			}
			if acc.PasswordHash != "" {
				release := password.Limit()
				valid := password.Check(acc.PasswordHash, c.Password)
				release()
				if !valid {
					writeJSON(w, 401, map[string]string{"error": "the password is wrong"})
					return
				}
			}
		} else {
			id, err := a.Store.OIDCIdentityOf(acc.ID)
			if err != nil || id.Issuer != a.OIDC.issuer {
				writeJSON(w, 409, map[string]string{"error": "no account is linked to this provider"})
				return
			}
		}
		flow.account, flow.session, flow.passwordHash = acc.ID, a.SessionKey(r), acc.PasswordHash
	}
	if err := a.OIDC.discover(r.Context()); err != nil {
		writeJSON(w, 502, map[string]string{"error": "cannot reach the login provider, please try again"})
		return
	}
	state := a.OIDC.flows.put(flow, a.Now(), a.Now().Add(flowLifetime))
	if state == "" {
		writeJSON(w, 503, map[string]string{"error": "too many pending logins"})
		return
	}
	a.setOIDCCookie(w, r, oidcStateCookie, state, flowLifetime)
	opts := []oauth2.AuthCodeOption{oidc.Nonce(flow.nonce), oauth2.S256ChallengeOption(flow.verifier)}
	if flow.mode == "reauth" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0"))
	}
	writeJSON(w, 200, map[string]string{"url": a.OIDC.config.AuthCodeURL(state, opts...)})
}

func (a *Service) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if !a.secure(r) {
		writeJSON(w, 403, map[string]string{"error": "openid login requires a secure connection"})
		return
	}
	o := a.OIDC
	w.Header().Set("Referrer-Policy", "no-referrer")
	back := func(result, reason string) { http.Redirect(w, r, o.home(result, reason), http.StatusFound) }
	q := r.URL.Query()
	state := a.cookie(r, oidcStateCookie)
	if len(q["state"]) != 1 || state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
		back("error", "expired")
		return
	}
	flow, ok := o.flows.get(state, a.Now(), true)
	a.setOIDCCookie(w, r, oidcStateCookie, "", 0)
	if !ok || len(q["code"]) > 1 || len(q["error"]) > 1 || (q.Has("iss") && (len(q["iss"]) != 1 || q.Get("iss") != o.issuer)) {
		back("error", "expired")
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		back("error", "denied")
		return
	}
	if flow.account != 0 {
		acc, valid := a.AccountFor(r)
		if !valid || acc.ID != flow.account || a.SessionKey(r) != flow.session {
			back("error", "expired")
			return
		}
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(r.Context(), o.client), 10*time.Second)
	defer cancel()
	token, err := o.config.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(flow.verifier))
	if err != nil {
		back("error", "failed")
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		back("error", "failed")
		return
	}
	idToken, err := o.provider.Verifier(&oidc.Config{ClientID: o.config.ClientID, Now: a.Now}).Verify(ctx, raw)
	if err != nil || idToken.Subject == "" || len(idToken.Subject) > 255 || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(flow.nonce)) != 1 {
		back("error", "failed")
		return
	}
	var claims struct {
		Name     string `json:"preferred_username"`
		AuthTime int64  `json:"auth_time"`
		AZP      string `json:"azp"`
	}
	if idToken.Claims(&claims) != nil || (claims.AZP != "" && claims.AZP != o.config.ClientID) || (len(idToken.Audience) > 1 && claims.AZP != o.config.ClientID) {
		back("error", "failed")
		return
	}
	if flow.mode == "reauth" && (claims.AuthTime < flow.started.Add(-time.Minute).Unix() || claims.AuthTime > a.Now().Add(time.Minute).Unix()) {
		back("error", "failed")
		return
	}
	id := store.OIDCIdentity{Issuer: o.issuer, Subject: idToken.Subject}
	var permanent string
	defer func() {
		if permanent != "" && a.OnPermanent != nil {
			a.OnPermanent(permanent)
		}
	}()
	o.mu.Lock()
	defer o.mu.Unlock()
	if flow.account != 0 {
		acc, valid := a.AccountFor(r)
		if !valid || acc.ID != flow.account || a.SessionKey(r) != flow.session || acc.PasswordHash != flow.passwordHash {
			back("error", "expired")
			return
		}
		if flow.mode == "reauth" {
			linked, err := a.Store.OIDCIdentityOf(acc.ID)
			if err != nil || linked != id {
				back("error", "other")
				return
			}
			for key, e := range o.proofs {
				if !a.Now().Before(e.expires) {
					delete(o.proofs, key)
				}
			}
			if len(o.proofs) >= maxFediPending {
				back("error", "failed")
				return
			}
			o.proofs[flow.session] = pendingEntry[store.OIDCIdentity]{id, a.Now().Add(reauthLifetime)}
			back("reauth", "")
			return
		}
		err := a.Store.LinkOIDC(acc.ID, flow.session, flow.passwordHash, id, a.Now())
		switch {
		case errors.Is(err, store.ErrOIDCTaken):
			back("error", "taken")
		case errors.Is(err, store.ErrSessionGone):
			back("error", "expired")
		case err != nil:
			back("error", "failed")
		default:
			// the deferred hook runs after the deferred unlock, even on panic.
			if acc.Temporary {
				permanent = acc.Name
			}
			back("linked", "")
		}
		return
	}
	acc, err := a.Store.AccountByOIDC(id)
	if err == nil {
		session := randomHex(32)
		if a.Store.CreateOIDCSession(tokenHash(session), acc.ID, id, a.Now().Add(sessionLifetime)) != nil {
			back("error", "failed")
			return
		}
		a.setSessionCookie(w, r, session)
		back("login", "")
		return
	}
	if !errors.Is(err, store.ErrNotFound) {
		back("error", "failed")
		return
	}
	key := o.signups.put(oidcSignup{id, fedi.SuggestName(claims.Name)}, a.Now(), a.Now().Add(signupLifetime))
	if key == "" {
		back("error", "failed")
		return
	}
	a.setOIDCCookie(w, r, oidcSignupCookie, key, signupLifetime)
	back("register", "")
}

func (a *Service) oidcSignup(w http.ResponseWriter, r *http.Request) {
	if !a.allow(r) {
		writeJSON(w, 429, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var c credentials
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if !a.mayRegister(w, c.Invite, false) {
		return
	}
	name, ok := a.checkName(w, c.Name)
	if !ok || !a.proven(w, c) {
		return
	}
	o := a.OIDC
	o.mu.Lock()
	defer o.mu.Unlock()
	key := a.cookie(r, oidcSignupCookie)
	s, ok := o.signups.get(key, a.Now(), false)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "the login expired, please try again"})
		return
	}
	acc, token, ok := a.newAccount(w, r, store.Account{Name: name}, true, func(acc store.Account) (store.Account, error) {
		return a.Store.CreateOIDCAccount(acc, s.identity, a.Now(), c.Invite)
	})
	if !ok {
		return
	}
	o.signups.get(key, a.Now(), true)
	a.setOIDCCookie(w, r, oidcSignupCookie, "", 0)
	writeJSON(w, 201, withToken(map[string]any{"name": acc.Name}, token))
}

func (a *Service) oidcUnlink(w http.ResponseWriter, r *http.Request) {
	if !a.allow(r) {
		writeJSON(w, 429, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	acc, ok := a.AccountFor(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not logged in"})
		return
	}
	var c struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if acc.PasswordHash == "" {
		writeJSON(w, 409, map[string]string{"error": "set a password first: this is your only way to log in"})
		return
	}
	release := password.Limit()
	ok = password.Check(acc.PasswordHash, c.Password)
	release()
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "the password is wrong"})
		return
	}
	keep := a.SessionKey(r)
	if err := a.endLogins(acc, keep, func() error {
		if err := a.Store.UnlinkOIDC(acc.ID, keep, acc.PasswordHash, a.Now()); err != nil {
			return err
		}
		if a.OIDC != nil {
			delete(a.OIDC.proofs, keep)
		}
		return nil
	}); err != nil {
		writeJSON(w, 409, map[string]string{"error": "the account or session changed, please try again"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *Service) oidcMe(acc store.Account) map[string]any {
	id, err := a.Store.OIDCIdentityOf(acc.ID)
	if err != nil {
		return nil
	}
	name := "OpenID Connect"
	enabled := a.OIDC != nil && a.OIDC.issuer == id.Issuer
	if enabled {
		name = a.OIDC.name
	}
	return map[string]any{"provider": name, "enabled": enabled}
}

func (a *Service) takeOIDCProof(session string, account int64) bool {
	if a.OIDC == nil {
		return false
	}
	o := a.OIDC
	o.mu.Lock()
	defer o.mu.Unlock()
	proof, ok := o.proofs[session]
	delete(o.proofs, session)
	if !ok || !a.Now().Before(proof.expires) {
		return false
	}
	id, err := a.Store.OIDCIdentityOf(account)
	return err == nil && id == proof.v && id.Issuer == o.issuer
}

// firstPasswordProof: a fresh login with a provider linked to the account,
// in this session (taken once): OIDC or fediverse.
func (a *Service) firstPasswordProof(acc store.Account, r *http.Request) bool {
	return a.takeOIDCProof(a.SessionKey(r), acc.ID) || a.Fedi != nil && a.Fedi.takeProof(a.SessionKey(r), acc.ID, a.Now())
}

// mayProve: a login linked to the account works here now, so it can give
// that proof; else only an admin's recovery link sets a first password.
func (a *Service) mayProve(acc store.Account) bool {
	if a.Fedi != nil {
		if _, err := a.Store.FediIdentityOf(acc.ID); err == nil {
			return true
		}
	}
	if a.OIDC != nil {
		if id, err := a.Store.OIDCIdentityOf(acc.ID); err == nil && id.Issuer == a.OIDC.issuer {
			return true
		}
	}
	return false
}
