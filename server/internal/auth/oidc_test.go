package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"kallisti.chat/server/internal/store"
)

// oidcIssuer exercises actual discovery, code exchange, pkce and jwks verification.
type oidcIssuer struct {
	server    *httptest.Server
	signer    jose.Signer
	public    jose.JSONWebKey
	mu        sync.Mutex
	codes     map[string]url.Values
	claims    func(map[string]any)
	discovery atomic.Int32
	exchanges atomic.Int32
}

func setupOIDC(t *testing.T) (*Service, *httptest.Server, *oidcIssuer) {
	t.Helper()
	a, _ := setup(t)
	srv := httptest.NewTLSServer(a)
	t.Cleanup(srv.Close)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
	if err != nil {
		t.Fatal(err)
	}
	p := &oidcIssuer{signer: signer, public: jose.JSONWebKey{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "ES256", Use: "sig"}, codes: make(map[string]url.Values)}
	p.server = httptest.NewTLSServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	a.OIDC, err = newOIDCLogin(p.server.URL, "client", "secret", "https://chat.example/api/oidc/callback", "Test ID", p.server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	return a, srv, p
}

func newOIDCBrowser(t *testing.T, srv *httptest.Server) *browser {
	t.Helper()
	b := newBrowser(t, srv)
	b.c.Transport = srv.Client().Transport
	return b
}

func (p *oidcIssuer) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		p.discovery.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize", "token_endpoint": p.server.URL + "/token", "jwks_uri": p.server.URL + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"ES256"}})
	case "/jwks":
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{p.public}})
	case "/token":
		p.exchanges.Add(1)
		_ = r.ParseForm()
		p.mu.Lock()
		q, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		alter := p.claims
		p.mu.Unlock()
		client, secret, _ := r.BasicAuth()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || client != "client" || secret != "secret" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != "https://chat.example/api/oidc/callback" || base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		claims := map[string]any{"iss": p.server.URL, "aud": "client", "sub": q.Get("subject"), "nonce": q.Get("nonce"), "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "auth_time": time.Now().Unix(), "preferred_username": "Alice"}
		if alter != nil {
			alter(claims)
		}
		raw, err := jwt.Signed(p.signer).Claims(claims).Serialize()
		if err != nil {
			http.Error(w, "signing failed", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "unused-private-token", "token_type": "Bearer", "expires_in": 3600, "id_token": raw})
	default:
		http.NotFound(w, r)
	}
}

func (p *oidcIssuer) approve(t *testing.T, raw, subject string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "https" || q.Get("response_type") != "code" || q.Get("client_id") != "client" || q.Get("nonce") == "" || q.Get("state") == "" || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 || q.Get("scope") != "openid profile" {
		t.Fatalf("invalid authorization request: %v", q)
	}
	q.Set("subject", subject)
	code := randomHex(16)
	p.mu.Lock()
	p.codes[code] = q
	p.mu.Unlock()
	return "/api/oidc/callback?" + url.Values{"state": {q.Get("state")}, "code": {code}, "iss": {p.server.URL}}.Encode()
}

func oidcBegin(t *testing.T, b *browser, p *oidcIssuer, mode, subject string) string {
	t.Helper()
	resp, v := b.do("POST", "/api/oidc/start", map[string]string{"mode": mode, "password": "longpassword"})
	if resp.StatusCode != 200 {
		t.Fatalf("start: %d %v", resp.StatusCode, v)
	}
	return p.approve(t, v["url"].(string), subject)
}

func oidcFinish(t *testing.T, b *browser, cb, result, reason string) {
	t.Helper()
	resp, _ := b.do("GET", cb, nil)
	q := url.Values{"oidc": {result}}
	if reason != "" {
		q.Set("reason", reason)
	}
	wantRedirect(t, resp, "https://chat.example/?"+q.Encode())
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("callback cache/referrer headers missing")
	}
}

func oidcRegister(t *testing.T, b *browser, p *oidcIssuer, subject, name string) {
	t.Helper()
	oidcFinish(t, b, oidcBegin(t, b, p, "login", subject), "register", "")
	resp, v := b.do("POST", "/api/oidc/signup", map[string]string{"name": name})
	if resp.StatusCode != 201 {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
}

func TestOIDCLoginSignupRepeatAndNoAutoLink(t *testing.T) {
	a, srv, p := setupOIDC(t)
	if p.discovery.Load() != 0 {
		t.Fatal("startup contacted provider")
	}
	b := newOIDCBrowser(t, srv)
	oidcRegister(t, b, p, "subject-1", "alice")
	_, me := b.do("GET", "/api/me", nil)
	if me["name"] != "alice" || me["password"] != false || me["oidc"].(map[string]any)["provider"] != "Test ID" {
		t.Fatalf("me: %v", me)
	}
	if resp, _ := b.do("POST", "/api/oidc/signup", map[string]string{"name": "reuse"}); resp.StatusCode != 404 {
		t.Fatalf("signup replay: %d", resp.StatusCode)
	}
	other := newOIDCBrowser(t, srv)
	oidcFinish(t, other, oidcBegin(t, other, p, "login", "subject-1"), "login", "")
	if _, v := other.do("GET", "/api/me", nil); v["name"] != "alice" {
		t.Fatalf("repeat login: %v", v)
	}
	third := newOIDCBrowser(t, srv)
	oidcFinish(t, third, oidcBegin(t, third, p, "login", "different-subject"), "register", "")
	if resp, _ := third.do("POST", "/api/oidc/signup", map[string]string{"name": "ALICE"}); resp.StatusCode != 409 {
		t.Fatalf("name collision: %d", resp.StatusCode)
	}
	if _, err := a.Store.AccountByOIDC(store.OIDCIdentity{Issuer: p.server.URL, Subject: "different-subject"}); err != store.ErrNotFound {
		t.Fatalf("auto linked by name: %v", err)
	}
	if resp, _ := third.do("POST", "/api/oidc/signup", map[string]string{"name": "alice2"}); resp.StatusCode != 201 {
		t.Fatalf("retry name: %d", resp.StatusCode)
	}
	if p.discovery.Load() != 1 {
		t.Fatalf("discovery count: %d", p.discovery.Load())
	}
}

func TestOIDCRejectsInvalidTokens(t *testing.T) {
	cases := map[string]func(map[string]any){
		"issuer":             func(c map[string]any) { c["iss"] = "https://evil.example" },
		"audience":           func(c map[string]any) { c["aud"] = "other-client" },
		"nonce":              func(c map[string]any) { c["nonce"] = "wrong" },
		"expired":            func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"subject":            func(c map[string]any) { delete(c, "sub") },
		"azp":                func(c map[string]any) { c["azp"] = "other-client" },
		"multiple-audiences": func(c map[string]any) { c["aud"] = []string{"client", "other"} },
		"signature":          nil,
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			_, srv, p := setupOIDC(t)
			p.claims = alter
			if name == "signature" {
				key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				p.signer, _ = jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
			}
			b := newOIDCBrowser(t, srv)
			oidcFinish(t, b, oidcBegin(t, b, p, "login", "subject"), "error", "failed")
			if resp, _ := b.do("GET", "/api/oidc/signup", nil); resp.StatusCode != 404 {
				t.Fatal("invalid token got signup")
			}
			if resp, _ := b.do("GET", "/api/me", nil); resp.StatusCode != 401 {
				t.Fatal("invalid token got session")
			}
		})
	}
}

func TestOIDCStateBrowserReplayAndIssuer(t *testing.T) {
	_, srv, p := setupOIDC(t)
	b := newOIDCBrowser(t, srv)
	cb := oidcBegin(t, b, p, "login", "subject")
	oidcFinish(t, newOIDCBrowser(t, srv), cb, "error", "expired")
	oidcFinish(t, b, strings.Replace(cb, "state=", "state=wrong", 1), "error", "expired")
	if p.exchanges.Load() != 0 {
		t.Fatal("invalid state exchanged code")
	}
	oidcFinish(t, b, cb, "register", "")
	oidcFinish(t, b, cb, "error", "expired")
	if p.exchanges.Load() != 1 {
		t.Fatal("callback replay exchanged code")
	}
	for _, extra := range []string{"&state=duplicate", "&iss=https%3A%2F%2Fevil.example", "&code=duplicate"} {
		cb = oidcBegin(t, b, p, "login", "subject")
		oidcFinish(t, b, cb+extra, "error", "expired")
	}
}

func TestOIDCInviteAndCaptchaAdmission(t *testing.T) {
	a, srv, p := setupOIDC(t)
	b := newOIDCBrowser(t, srv)
	oidcFinish(t, b, oidcBegin(t, b, p, "login", "subject"), "register", "")
	if err := a.Store.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	if resp, v := b.do("POST", "/api/oidc/signup", map[string]string{"name": "alice"}); resp.StatusCode != 403 || v["code"] != "invite_required" {
		t.Fatalf("invite: %d %v", resp.StatusCode, v)
	}
	if err := a.Store.AddRegistrationInvite("invite", store.RegistrationInvite{Creator: "admin", Created: a.Now().UnixMilli(), MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	captchaBits["oidc-test"] = 8
	t.Cleanup(func() { delete(captchaBits, "oidc-test") })
	a.Captcha = func() string { return "oidc-test" }
	if resp, _ := b.do("POST", "/api/oidc/signup", map[string]string{"name": "alice", "invite": "invite"}); resp.StatusCode != 403 {
		t.Fatalf("captcha: %d", resp.StatusCode)
	}
	_, challengeResponse := b.do("GET", "/api/challenge", nil)
	challenge, bits := challengeResponse["challenge"].(string), int(challengeResponse["bits"].(float64))
	if resp, v := b.do("POST", "/api/oidc/signup", map[string]string{"name": "alice", "invite": "invite", "challenge": challenge, "nonce": solve(challenge, bits)}); resp.StatusCode != 201 {
		t.Fatalf("admitted: %d %v", resp.StatusCode, v)
	}
	other := newOIDCBrowser(t, srv)
	oidcFinish(t, other, oidcBegin(t, other, p, "login", "subject"), "login", "")
	third := newOIDCBrowser(t, srv)
	oidcFinish(t, third, oidcBegin(t, third, p, "login", "subject2"), "register", "")
	if resp, _ := third.do("POST", "/api/oidc/signup", map[string]string{"name": "other", "invite": "invite"}); resp.StatusCode != 403 {
		t.Fatalf("invite replay: %d", resp.StatusCode)
	}
}

func TestOIDCFirstPasswordReauthAndUnlink(t *testing.T) {
	a, srv, p := setupOIDC(t)
	b := newOIDCBrowser(t, srv)
	oidcRegister(t, b, p, "subject", "alice")
	if resp, _ := b.do("DELETE", "/api/oidc/link", map[string]string{"password": "longpassword"}); resp.StatusCode != 409 {
		t.Fatalf("unlink only method: %d", resp.StatusCode)
	}
	for _, disabled := range []bool{false, true} {
		configured := a.OIDC
		if disabled {
			a.OIDC = nil
		}
		// Without the provider no proof can come: an admin's recovery link.
		if resp, v := b.do("POST", "/api/password", map[string]string{"new": "longpassword"}); resp.StatusCode != 403 || (v["code"] == "reauth") == disabled {
			t.Fatalf("password without proof (disabled=%v): %d %v", disabled, resp.StatusCode, v)
		}
		a.OIDC = configured
	}
	oidcFinish(t, b, oidcBegin(t, b, p, "reauth", "different-subject"), "error", "other")
	oidcFinish(t, b, oidcBegin(t, b, p, "reauth", "subject"), "reauth", "")
	other := newOIDCBrowser(t, srv)
	oidcFinish(t, other, oidcBegin(t, other, p, "login", "subject"), "login", "")
	if resp, _ := other.do("POST", "/api/password", map[string]string{"new": "longpassword"}); resp.StatusCode != 403 {
		t.Fatal("reauth proof crossed sessions")
	}
	if resp, v := b.do("POST", "/api/password", map[string]string{"new": "longpassword"}); resp.StatusCode != 204 {
		t.Fatalf("first password: %d %v", resp.StatusCode, v)
	}
	if resp, _ := other.do("GET", "/api/me", nil); resp.StatusCode != 401 {
		t.Fatal("password did not revoke other session")
	}
	a.logins = limiter{}
	if resp, _ := b.do("DELETE", "/api/oidc/link", map[string]string{"password": "incorrect"}); resp.StatusCode != 401 {
		t.Fatalf("wrong unlink password: %d", resp.StatusCode)
	}
	a.OIDC = nil
	if _, v := b.do("GET", "/api/me", nil); v["oidc"].(map[string]any)["enabled"] != false {
		t.Fatal("disabled provider not reported")
	}
	if resp, v := b.do("DELETE", "/api/oidc/link", map[string]string{"password": "longpassword"}); resp.StatusCode != 204 {
		t.Fatalf("disabled provider unlink: %d %v", resp.StatusCode, v)
	}
	if _, v := b.do("GET", "/api/me", nil); v["oidc"] != nil {
		t.Fatal("identity survived unlink")
	}
	if resp, _ := other.do("POST", "/api/login", map[string]string{"name": "alice", "password": "longpassword"}); resp.StatusCode != 200 {
		t.Fatal("password login after unlink failed")
	}
}

func TestOIDCReauthFreshnessAndSingleUse(t *testing.T) {
	for _, kind := range []string{"old", "missing", "future", "expired-proof", "used-proof"} {
		t.Run(kind, func(t *testing.T) {
			a, srv, p := setupOIDC(t)
			b := newOIDCBrowser(t, srv)
			oidcRegister(t, b, p, "subject", "alice")
			if kind == "old" || kind == "missing" || kind == "future" {
				p.mu.Lock()
				p.claims = func(c map[string]any) {
					switch kind {
					case "old":
						c["auth_time"] = time.Now().Add(-time.Hour).Unix()
					case "future":
						c["auth_time"] = time.Now().Add(time.Hour).Unix()
					case "missing":
						delete(c, "auth_time")
					}
				}
				p.mu.Unlock()
				oidcFinish(t, b, oidcBegin(t, b, p, "reauth", "subject"), "error", "failed")
			} else {
				oidcFinish(t, b, oidcBegin(t, b, p, "reauth", "subject"), "reauth", "")
				acc, _ := a.Store.AccountByName("alice")
				u, _ := url.Parse(srv.URL)
				cookies := b.c.Jar.Cookies(u)
				var session string
				for _, c := range cookies {
					if c.Name == "__Host-"+CookieName {
						session = tokenHash(c.Value)
					}
				}
				if kind == "used-proof" && !a.takeOIDCProof(session, acc.ID) {
					t.Fatal("missing proof")
				}
				if kind == "expired-proof" {
					a.OIDC.mu.Lock()
					entry := a.OIDC.proofs[session]
					entry.expires = time.Now().Add(-time.Second)
					a.OIDC.proofs[session] = entry
					a.OIDC.mu.Unlock()
				}
			}
			if resp, _ := b.do("POST", "/api/password", map[string]string{"new": "longpassword"}); resp.StatusCode != 403 {
				t.Fatalf("invalid proof accepted: %d", resp.StatusCode)
			}
		})
	}
}

func TestOIDCLinkChecksPasswordAndSession(t *testing.T) {
	for _, change := range []string{"none", "password", "logout", "different-session"} {
		t.Run(change, func(t *testing.T) {
			a, srv, p := setupOIDC(t)
			b := newOIDCBrowser(t, srv)
			resp, _ := b.do("POST", "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"})
			if resp.StatusCode != 201 {
				t.Fatal(resp.StatusCode)
			}
			if resp, _ := b.do("POST", "/api/oidc/start", map[string]string{"mode": "link", "password": "wrong"}); resp.StatusCode != 401 {
				t.Fatalf("wrong link password: %d", resp.StatusCode)
			}
			cb := oidcBegin(t, b, p, "link", "subject")
			switch change {
			case "password":
				if resp, _ := b.do("POST", "/api/password", map[string]string{"old": "longpassword", "new": "newpassword"}); resp.StatusCode != 204 {
					t.Fatal(resp.StatusCode)
				}
			case "logout":
				b.do("POST", "/api/logout", map[string]string{})
			case "different-session":
				b.do("POST", "/api/login", map[string]string{"name": "alice", "password": "longpassword"})
			}
			if change == "none" {
				oidcFinish(t, b, cb, "linked", "")
				other := newOIDCBrowser(t, srv)
				oidcFinish(t, other, oidcBegin(t, other, p, "login", "subject"), "login", "")
			} else {
				oidcFinish(t, b, cb, "error", "expired")
				if _, err := a.Store.AccountByOIDC(store.OIDCIdentity{Issuer: p.server.URL, Subject: "subject"}); err != store.ErrNotFound {
					t.Fatal("stale link succeeded")
				}
			}
		})
	}
}

func TestOIDCGuestLinkAndFediOnlyGuard(t *testing.T) {
	a, srv, p := setupOIDC(t)
	a.GuestAccounts = true
	permanent := make(chan string, 1)
	a.OnPermanent = func(name string) {
		// hooks may reenter revocation; they must run outside the oidc lock.
		acc, err := a.Store.AccountByName(name)
		if err == nil {
			_ = a.endLogins(acc, "", func() error { return nil })
		}
		permanent <- name
	}
	b := newOIDCBrowser(t, srv)
	if resp, _ := b.do("POST", "/api/guest", map[string]string{"name": "guest"}); resp.StatusCode != 201 {
		t.Fatal(resp.StatusCode)
	}
	oidcFinish(t, b, oidcBegin(t, b, p, "link", "subject"), "linked", "")
	select {
	case name := <-permanent:
		if name != "guest" {
			t.Fatal(name)
		}
	case <-time.After(time.Second):
		t.Fatal("permanent hook missing")
	}
	acc, _ := a.Store.AccountByName("guest")
	if acc.Temporary {
		t.Fatal("linked guest stayed temporary")
	}
	if resp, _ := b.do("POST", "/api/oidc/start", map[string]string{"mode": "link"}); resp.StatusCode != 409 {
		t.Fatal("oidc-only account could link without password")
	}
	u, _ := url.Parse(srv.URL)
	var session string
	for _, c := range b.c.Jar.Cookies(u) {
		if c.Name == "__Host-"+CookieName {
			session = tokenHash(c.Value)
		}
	}
	identity := store.FediIdentity{Host: "social.example", RemoteID: "42", Handle: "@attacker@social.example"}
	if result, reason := a.fediLink(acc, session, identity); result != "error" || reason != "reauth" {
		t.Fatalf("fedi bypass: %s %s", result, reason)
	}
	oidcFinish(t, b, oidcBegin(t, b, p, "reauth", "subject"), "reauth", "")
	if result, reason := a.fediLink(acc, session, identity); result != "linked" {
		t.Fatalf("fedi after proof: %s %s", result, reason)
	}
}

func TestOIDCConcurrentCallbacksAndSignup(t *testing.T) {
	a, srv, p := setupOIDC(t)
	b := newOIDCBrowser(t, srv)
	cb := oidcBegin(t, b, p, "login", "subject")
	u, _ := url.Parse(srv.URL)
	cookies := b.c.Jar.Cookies(u)
	var wg sync.WaitGroup
	codes := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			r := httptest.NewRequest("GET", srv.URL+cb, nil)
			for _, c := range cookies {
				r.AddCookie(c)
			}
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			codes <- w.Header().Get("Location")
		})
	}
	wg.Wait()
	close(codes)
	successful := 0
	for result := range codes {
		if strings.Contains(result, "oidc=register") {
			successful++
		}
	}
	if successful != 1 || p.exchanges.Load() != 1 {
		t.Fatalf("callback winners=%d exchanges=%d", successful, p.exchanges.Load())
	}
	// two independently authenticated browsers cannot create two accounts for one subject.
	b1, b2 := newOIDCBrowser(t, srv), newOIDCBrowser(t, srv)
	for _, b := range []*browser{b1, b2} {
		oidcFinish(t, b, oidcBegin(t, b, p, "login", "subject"), "register", "")
	}
	statuses := make(chan int, 2)
	for i, b := range []*browser{b1, b2} {
		wg.Go(func() {
			name := []string{"one", "two"}[i]
			req := httptest.NewRequest("POST", srv.URL+"/api/oidc/signup", strings.NewReader(`{"name":"`+name+`"}`))
			req.Header.Set("Content-Type", "application/json")
			for _, c := range b.c.Jar.Cookies(u) {
				req.AddCookie(c)
			}
			w := httptest.NewRecorder()
			a.ServeHTTP(w, req)
			statuses <- w.Code
		})
	}
	wg.Wait()
	close(statuses)
	seen := map[int]int{}
	for code := range statuses {
		seen[code]++
	}
	if seen[201] != 1 || seen[409] != 1 {
		t.Fatalf("signup results: %v", seen)
	}
}

func TestOIDCDiscoveryFailureKeepsOtherAuthWorking(t *testing.T) {
	a, srv, p := setupOIDC(t)
	p.server.Close()
	b := newOIDCBrowser(t, srv)
	if resp, _ := b.do("POST", "/api/oidc/start", map[string]string{}); resp.StatusCode != 502 {
		t.Fatalf("offline provider: %d", resp.StatusCode)
	}
	if resp, _ := b.do("POST", "/api/accounts", map[string]string{"name": "local", "password": "longpassword"}); resp.StatusCode != 201 {
		t.Fatalf("local registration: %d", resp.StatusCode)
	}
	if resp, _ := b.do("POST", "/api/login", map[string]string{"name": "local", "password": "longpassword"}); resp.StatusCode != 200 {
		t.Fatalf("local login: %d", resp.StatusCode)
	}
	a.GuestAccounts = true
	if resp, _ := newOIDCBrowser(t, srv).do("POST", "/api/guest", map[string]string{"name": "guest"}); resp.StatusCode != 201 {
		t.Fatalf("guest login: %d", resp.StatusCode)
	}
}

func TestOIDCRequiresSecureTransport(t *testing.T) {
	a, _, p := setupOIDC(t)
	for _, path := range []string{"/api/oidc/start", "/api/oidc/callback?state=attacker&code=code"} {
		method := "POST"
		if strings.Contains(path, "callback") {
			method = "GET"
		}
		r := httptest.NewRequest(method, "http://chat.example"+path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "attacker"})
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("insecure request: %s status=%d cookies=%v", path, w.Code, w.Header().Values("Set-Cookie"))
		}
	}
	if p.discovery.Load() != 0 || p.exchanges.Load() != 0 {
		t.Fatal("insecure request contacted provider")
	}
	// explicitly trusting the TLS-terminating proxy restores secure cookies.
	a.TrustForwardedFor = true
	r := httptest.NewRequest("POST", "http://chat.example/api/oidc/start", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-"+oidcStateCookie || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Domain != "" || cookies[0].Path != "/" {
		t.Fatalf("unsafe proxy cookie: %v", cookies)
	}
}

func TestOIDCRejectsForeignOrigin(t *testing.T) {
	a, _, p := setupOIDC(t)
	for _, path := range []string{"/api/oidc/start", "/api/oidc/signup", "/api/oidc/link"} {
		method := "POST"
		if strings.HasSuffix(path, "/link") {
			method = "DELETE"
		}
		r := httptest.NewRequest(method, "https://chat.example"+path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://attacker.example")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("foreign origin %s: %d", path, w.Code)
		}
	}
	if p.discovery.Load() != 0 {
		t.Fatal("foreign origin contacted provider")
	}
}

func TestOIDCUnlinkRevokesOtherSessionsAndConnections(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "enabled", true: "disabled"}[disabled], func(t *testing.T) {
			a, srv, p := setupOIDC(t)
			b := newOIDCBrowser(t, srv)
			if resp, _ := b.do("POST", "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}); resp.StatusCode != 201 {
				t.Fatal(resp.StatusCode)
			}
			oidcFinish(t, b, oidcBegin(t, b, p, "link", "subject"), "linked", "")
			other := newOIDCBrowser(t, srv)
			oidcFinish(t, other, oidcBegin(t, other, p, "login", "subject"), "login", "")
			acc, _ := a.Store.AccountByName("alice")
			u, _ := url.Parse(srv.URL)
			r := httptest.NewRequest("GET", srv.URL+"/api/me", nil)
			for _, c := range b.c.Jar.Cookies(u) {
				r.AddCookie(c)
			}
			keep := a.SessionKey(r)
			revoked := make(chan bool, 1)
			a.Revoke = func(match func(int64, string) bool) {
				revoked <- !match(acc.ID, keep) && match(acc.ID, "other") && !match(acc.ID+1, "other")
			}
			if disabled {
				a.OIDC = nil
			}
			if resp, _ := b.do("DELETE", "/api/oidc/link", map[string]string{"password": "longpassword"}); resp.StatusCode != 204 {
				t.Fatal(resp.StatusCode)
			}
			select {
			case ok := <-revoked:
				if !ok {
					t.Fatal("incorrect connection revocation")
				}
			default:
				t.Fatal("connection revocation missing")
			}
			if resp, _ := other.do("GET", "/api/me", nil); resp.StatusCode != 401 {
				t.Fatal("other session survived unlink")
			}
			if resp, _ := b.do("GET", "/api/me", nil); resp.StatusCode != 200 {
				t.Fatal("current session revoked")
			}
		})
	}
}

func TestOIDCPermanentHookPanicReleasesLock(t *testing.T) {
	a, srv, p := setupOIDC(t)
	a.GuestAccounts = true
	b := newOIDCBrowser(t, srv)
	if resp, _ := b.do("POST", "/api/guest", map[string]string{"name": "guest"}); resp.StatusCode != 201 {
		t.Fatal(resp.StatusCode)
	}
	cb := oidcBegin(t, b, p, "link", "subject")
	a.OnPermanent = func(string) { panic("hook panic") }
	r := httptest.NewRequest("GET", srv.URL+cb, nil)
	u, _ := url.Parse(srv.URL)
	for _, c := range b.c.Jar.Cookies(u) {
		r.AddCookie(c)
	}
	func() {
		defer func() {
			if got := recover(); got != "hook panic" {
				t.Fatalf("unexpected panic: %v", got)
			}
		}()
		a.ServeHTTP(httptest.NewRecorder(), r)
	}()
	if !a.OIDC.mu.TryLock() {
		t.Fatal("panic left identity lock held")
	}
	a.OIDC.mu.Unlock()
	if _, err := a.Store.AccountByOIDC(store.OIDCIdentity{Issuer: p.server.URL, Subject: "subject"}); err != nil {
		t.Fatal("hook panic lost committed link")
	}
}
