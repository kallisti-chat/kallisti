package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/store"
)

func setup(t *testing.T) (*Service, *httptest.Server) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := &Service{Store: s, Now: time.Now, AllowedOrigins: []string{"https://app.example"}}
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	return a, srv
}

func post(t *testing.T, srv *httptest.Server, path string, body any, cookie *http.Cookie) (*http.Response, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.example")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	return resp, v
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	return nil
}

func requestWith(c *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "/ws", nil)
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

func TestCreateAccountLogsIn(t *testing.T) {
	a, srv := setup(t)
	resp, v := post(t, srv, "/api/accounts", map[string]string{"name": " Alice ", "password": "correct horse"}, nil)
	if resp.StatusCode != http.StatusCreated || v["name"] != "Alice" {
		t.Fatalf("create: %d %v", resp.StatusCode, v)
	}
	c := sessionCookie(resp)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie: %+v", c)
	}
	if resp.Header.Get("Access-Control-Allow-Credentials") != "true" || resp.Header.Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Errorf("CORS: %v", resp.Header)
	}
	acc, ok := a.AccountFor(requestWith(c))
	if !ok || acc.Name != "Alice" || len(acc.CertPEM) == 0 {
		t.Fatalf("account for cookie: %+v %v", acc, ok)
	}
	if strings.Contains(acc.PasswordHash, "correct horse") || !strings.HasPrefix(acc.PasswordHash, "$argon2id$") {
		t.Fatalf("password hash: %q", acc.PasswordHash)
	}
}

func TestLoginLogoutAndMe(t *testing.T) {
	a, srv := setup(t)
	post(t, srv, "/api/accounts", map[string]string{"name": "bob", "password": "longpassword"}, nil)
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "BOB", "password": "wrong password"}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "nobody", "password": "longpassword"}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown name: %d", resp.StatusCode)
	}
	resp, v := post(t, srv, "/api/login", map[string]string{"name": "BOB", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	if resp.StatusCode != http.StatusOK || v["name"] != "bob" || c == nil {
		t.Fatalf("login: %d %v", resp.StatusCode, v)
	}
	me := httptest.NewRecorder()
	a.ServeHTTP(me, func() *http.Request { r := httptest.NewRequest("GET", "/api/me", nil); r.AddCookie(c); return r }())
	if me.Code != 200 || !strings.Contains(me.Body.String(), `"bob"`) {
		t.Fatalf("me: %d %s", me.Code, me.Body.String())
	}
	if resp, _ := post(t, srv, "/api/logout", nil, c); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if _, ok := a.AccountFor(requestWith(c)); ok {
		t.Fatal("session still valid after logout")
	}
}

func TestValidation(t *testing.T) {
	_, srv := setup(t)
	post(t, srv, "/api/accounts", map[string]string{"name": "carol", "password": "longpassword"}, nil)
	cases := []map[string]string{
		{"name": "", "password": "longpassword"},                      // empty
		{"name": strings.Repeat("n", 33), "password": "longpassword"}, // too long
		{"name": "bad\nname", "password": "longpassword"},             // control character
		{"name": "superuser", "password": "longpassword"},             // Murmur's admin
		{"name": "dave", "password": "short"},                         // weak password
		{"name": "CAROL", "password": "longpassword"},                 // taken
	}
	for _, c := range cases {
		if resp, v := post(t, srv, "/api/accounts", c, nil); resp.StatusCode < 400 || v["error"] == nil {
			t.Errorf("%q: %d %v", c["name"], resp.StatusCode, v)
		}
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, srv := setup(t)
	limited := false
	for i := 0; i < burst+3; i++ {
		resp, _ := post(t, srv, "/api/login", map[string]string{"name": "x", "password": "yyyyyyyy"}, nil)
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("no login attempt was limited")
	}
}

func TestCrossSiteFormsAreRefused(t *testing.T) {
	_, srv := setup(t)
	// A plain HTML form can send text/plain with a JSON body, without CORS.
	req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(`{"name":"x","password":"yyyyyyyy"}`))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d, want 415", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/api/logout", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site: %d, want 403", resp.StatusCode)
	}
}

func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	a, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "erin", "password": "old password"}, nil)
	first := sessionCookie(resp)
	resp, _ = post(t, srv, "/api/login", map[string]string{"name": "erin", "password": "old password"}, nil)
	second := sessionCookie(resp)
	if resp, v := post(t, srv, "/api/password", map[string]string{"old": "wrong", "new": "new password"}, second); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong old password: %d %v", resp.StatusCode, v)
	}
	if resp, v := post(t, srv, "/api/password", map[string]string{"old": "old password", "new": "new password"}, second); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change: %d %v", resp.StatusCode, v)
	}
	if _, ok := a.AccountFor(requestWith(first)); ok {
		t.Error("other session still valid")
	}
	if _, ok := a.AccountFor(requestWith(second)); !ok {
		t.Error("the changing session was ended")
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "erin", "password": "new password"}, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("login with the new password: %d", resp.StatusCode)
	}
}

// stalledBody tells when the handler starts to read it, then waits for
// the body text.
type stalledBody struct {
	reading chan struct{}
	text    chan string
	rest    *strings.Reader
}

func (b *stalledBody) Read(p []byte) (int, error) {
	if b.rest == nil {
		close(b.reading)
		b.rest = strings.NewReader(<-b.text)
	}
	return b.rest.Read(p)
}

// A password change checks the password and session when it saves, not
// when the request starts: an owner's change in between wins.
func TestStalledPasswordChangeLosesToTheOwner(t *testing.T) {
	a, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "ivan", "password": "old password"}, nil)
	owner := sessionCookie(resp)
	resp, _ = post(t, srv, "/api/login", map[string]string{"name": "ivan", "password": "old password"}, nil)
	attacker := sessionCookie(resp)

	body := &stalledBody{reading: make(chan struct{}), text: make(chan string)}
	r := httptest.NewRequest("POST", "/api/password", body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://app.example")
	r.AddCookie(attacker)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { a.ServeHTTP(rec, r); close(done) }()
	<-body.reading

	if resp, v := post(t, srv, "/api/password", map[string]string{"old": "old password", "new": "owner password"}, owner); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("owner change: %d %v", resp.StatusCode, v)
	}
	body.text <- `{"old":"old password","new":"attacker password"}`
	<-done
	if rec.Code == http.StatusNoContent {
		t.Errorf("stalled change: %d, want a refusal", rec.Code)
	}
	if _, ok := a.AccountFor(requestWith(owner)); !ok {
		t.Error("the owner's session was ended")
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "ivan", "password": "attacker password"}, nil); resp.StatusCode == http.StatusOK {
		t.Error("login with the attacker's password")
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "ivan", "password": "owner password"}, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("login with the owner's password: %d", resp.StatusCode)
	}
}

func TestLogoutEverywhere(t *testing.T) {
	a, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "frank", "password": "longpassword"}, nil)
	first := sessionCookie(resp)
	resp, _ = post(t, srv, "/api/login", map[string]string{"name": "frank", "password": "longpassword"}, nil)
	second := sessionCookie(resp)
	if resp, _ := post(t, srv, "/api/logout-all", map[string]string{}, second); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout-all: %d", resp.StatusCode)
	}
	for _, c := range []*http.Cookie{first, second} {
		if _, ok := a.AccountFor(requestWith(c)); ok {
			t.Error("a session is still valid")
		}
	}
}

// Ending sessions also ends their open connections (Revoke).
func TestEndedSessionsAreRevoked(t *testing.T) {
	a, srv := setup(t)
	var match func(int64, string) bool
	a.Revoke = func(m func(int64, string) bool) { match = m }
	var ended []string // Mumble clients logged in with the password
	a.EndPasswordLogins = func(acc store.Account) { ended = append(ended, acc.Name) }
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "gina", "password": "longpassword"}, nil)
	first := sessionCookie(resp)
	acc, _ := a.AccountFor(requestWith(first))
	login := func() *http.Cookie {
		resp, _ := post(t, srv, "/api/login", map[string]string{"name": "gina", "password": "longpassword"}, nil)
		return sessionCookie(resp)
	}
	second, third := login(), login()
	check := func(what string, want map[*http.Cookie]bool, mumble []string) {
		t.Helper()
		if !slices.Equal(ended, mumble) {
			t.Errorf("%s: password logins ended %v, want %v", what, ended, mumble)
		}
		ended = nil
		if match == nil {
			t.Fatalf("%s: nothing revoked", what)
		}
		for c, revoked := range want {
			if match(acc.ID, (&Service{}).SessionKey(requestWith(c))) != revoked {
				t.Errorf("%s: session revoked %v, want %v", what, !revoked, revoked)
			}
		}
		if match(acc.ID+1, "") {
			t.Errorf("%s: another account revoked", what)
		}
		match = nil
	}
	post(t, srv, "/api/logout", nil, third)
	check("logout", map[*http.Cookie]bool{third: true, second: false}, nil)
	post(t, srv, "/api/password", map[string]string{"old": "longpassword", "new": "new password"}, second)
	check("password change", map[*http.Cookie]bool{first: true, second: false}, []string{"gina"})
	post(t, srv, "/api/logout-all", map[string]string{}, second)
	check("logout-all", map[*http.Cookie]bool{first: true, second: true}, []string{"gina"})
}

// An admin deletes an account (removing its registration): its sessions
// and logins end, and its name is free.
func TestDeleteAccount(t *testing.T) {
	a, srv := setup(t)
	var match func(int64, string) bool
	a.Revoke = func(m func(int64, string) bool) { match = m }
	var ended []string
	a.EndPasswordLogins = func(acc store.Account) { ended = append(ended, acc.Name) }
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "gina", "password": "longpassword"}, nil)
	cookie := sessionCookie(resp)
	acc, _ := a.AccountFor(requestWith(cookie))
	if err := a.DeleteAccount(acc); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.AccountFor(requestWith(cookie)); ok {
		t.Error("the session lives on")
	}
	if _, err := a.Store.AccountByName("gina"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("account: %v", err)
	}
	if match == nil || !match(acc.ID, "") || match(acc.ID+1, "") || !slices.Equal(ended, []string{"gina"}) {
		t.Errorf("logins not ended: %v", ended)
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "gina", "password": "longpassword"}, nil); resp.StatusCode == http.StatusOK {
		t.Error("logged in again")
	}
}

func TestExpiredSessionsAreRevoked(t *testing.T) {
	a, srv := setup(t)
	now := time.Now()
	a.Now = func() time.Time { return now }
	var match func(int64, string) bool
	a.Revoke = func(m func(int64, string) bool) { match = m }
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "hank", "password": "longpassword"}, nil)
	old := (&Service{}).SessionKey(requestWith(sessionCookie(resp)))
	acc, _ := a.Store.AccountBySession(old, now)
	now = now.Add(sessionLifetime / 2)
	resp, _ = post(t, srv, "/api/login", map[string]string{"name": "hank", "password": "longpassword"}, nil)
	current := (&Service{}).SessionKey(requestWith(sessionCookie(resp)))
	now = now.Add(sessionLifetime / 2)
	if err := a.DeleteExpiredSessions(); err != nil {
		t.Fatal(err)
	}
	if match == nil || !match(acc.ID, old) || match(acc.ID, current) {
		t.Fatal("want only the expired session revoked")
	}
}

// Other sites (another Kallisti's page, the apps) log in with a token: CORS
// without credentials, and a cross-site request's cookie does not count.
func TestOtherSitesUseTokens(t *testing.T) {
	a, srv := setup(t)
	a.AllowedOrigins = []string{"https://chat.example"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/api/login", nil)
	r.Header.Set("Origin", "https://other-kallisti.example")
	a.cors(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "https://other-kallisti.example" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("cors %v", w.Header())
	}
	post := func(path, origin string) int {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(`{"name":"x","password":"y"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		return resp.StatusCode
	}
	if post("/api/login", "https://other-kallisti.example") == http.StatusForbidden {
		t.Fatal("a token login from another site was refused")
	}
	// Guest accounts only on our own pages and in the apps: else any site
	// could make its visitors fill the server with guests.
	a.GuestAccounts = true
	if got := post("/api/guest", "https://other-kallisti.example"); got != http.StatusForbidden {
		t.Fatalf("a guest from another site: %d, want 403", got)
	}
	if got := post("/api/guest", "tauri://localhost"); got == http.StatusForbidden {
		t.Fatalf("a guest in the app: %d", got)
	}
	// New accounts too (they would stay for ever); other servers' users
	// come with their home login, existing accounts log in from anywhere.
	if got := post("/api/accounts", "https://other-kallisti.example"); got != http.StatusForbidden {
		t.Fatalf("an account from another site: %d, want 403", got)
	}
	if got := post("/api/accounts", "tauri://localhost"); got == http.StatusForbidden {
		t.Fatalf("an account in the app: %d", got)
	}
	// The cookie counts only without an origin or from our own: not from a
	// sibling subdomain (same site: the browser sends the cookie), nor from
	// another site.
	for origin, counts := range map[string]bool{
		"":                               true, // a navigation (the fedi callback is cross-site, without an origin)
		"https://chat.example":           true,
		"https://test.chat.example":      false,
		"https://other-kallisti.example": false,
	} {
		r := httptest.NewRequest("GET", "/api/me", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.AddCookie(&http.Cookie{Name: CookieName, Value: "x"})
		if got := a.SessionKey(r) != ""; got != counts {
			t.Errorf("%q: cookie counted %v, want %v", origin, got, counts)
		}
	}
}

func TestWildcardOriginsGetCredentialsOnlyForLocalhost(t *testing.T) {
	a := &Service{AllowedOrigins: []string{"*"}}
	for origin, want := range map[string]bool{
		"http://localhost:5173":         true,
		"http://127.0.0.1:5173":         true,
		"https://evil.example":          false,
		"http://localhost.evil.example": false,
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("OPTIONS", "/api/login", nil)
		r.Header.Set("Origin", origin)
		a.cors(w, r)
		if got := w.Header().Get("Access-Control-Allow-Credentials") == "true"; got != want {
			t.Errorf("%s: credentials %v, want %v", origin, got, want)
		}
	}
}

func TestNamesTakenElsewhereAreRefused(t *testing.T) {
	a, srv := setup(t)
	a.NameTaken = func(name string) bool { return name == "bob" } // registered in Mumble
	resp, body := post(t, srv, "/api/accounts", map[string]string{"name": "Bob", "password": "long enough"}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d %v", resp.StatusCode, body)
	}
}

func TestLoginWithAMumbleRegistration(t *testing.T) {
	a, srv := setup(t)
	a.MumbleLogin = func(_ netip.Addr, name, pw string) (string, bool) {
		return "SuperUser", name == "superuser" && pw == "adminpw"
	}
	resp, v := post(t, srv, "/api/login", map[string]string{"name": "superuser", "password": "adminpw"}, nil)
	if resp.StatusCode != http.StatusOK || v["name"] != "SuperUser" || v["mumble"] != true || sessionCookie(resp) != nil && sessionCookie(resp).Value != "" {
		t.Fatalf("mumble login: %d %v", resp.StatusCode, v)
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "superuser", "password": "nope"}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	// A web account comes first, also with a wrong password.
	resp, _ = post(t, srv, "/api/accounts", map[string]string{"name": "bob", "password": "longpassword"}, nil)
	// bob's session in this browser ends with a Mumble login.
	bob := sessionCookie(resp)
	var match func(int64, string) bool
	a.Revoke = func(m func(int64, string) bool) { match = m }
	post(t, srv, "/api/login", map[string]string{"name": "superuser", "password": "adminpw"}, bob)
	if _, ok := a.AccountFor(requestWith(bob)); ok {
		t.Fatal("web session kept")
	}
	if match == nil || !match(0, (&Service{}).SessionKey(requestWith(bob))) {
		t.Fatal("the web session's connections were not revoked")
	}
	a.MumbleLogin = func(netip.Addr, string, string) (string, bool) { return "bob", true }
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "bob", "password": "wrong password"}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("web account: %d", resp.StatusCode)
	}
}

// The desktop app (another origin: the cookie is third-party there) asks
// for its session token and sends it in a header, or on the WebSocket as a
// subprotocol.
func TestDesktopAppTokens(t *testing.T) {
	_, srv := setup(t)
	post(t, srv, "/api/accounts", map[string]string{"name": "lain", "password": "long enough"}, nil)
	login := func(want bool) map[string]any {
		b, _ := json.Marshal(map[string]string{"name": "lain", "password": "long enough"})
		req, _ := http.NewRequest("POST", srv.URL+"/api/login", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "tauri://localhost")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		if want {
			req.Header.Set("X-Kallisti-Session", "token")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var v map[string]any
		json.NewDecoder(resp.Body).Decode(&v)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login from the app: %d %v", resp.StatusCode, v)
		}
		return v
	}
	if v := login(false); v["token"] != nil {
		t.Fatalf("token without asking: %v", v)
	}
	token, _ := login(true)["token"].(string)
	if token == "" {
		t.Fatal("no token")
	}
	me, _ := http.NewRequest("GET", srv.URL+"/api/me", nil)
	me.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(me)
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	resp.Body.Close()
	if v["name"] != "lain" {
		t.Fatalf("me with the token: %v", v)
	}
	ws := httptest.NewRequest("GET", "/", nil)
	ws.Header.Set("Sec-WebSocket-Protocol", "kallisti, kallisti.session."+token)
	if (&Service{}).SessionKey(ws) != tokenHash(token) {
		t.Fatal("the WebSocket subprotocol carries no session")
	}
}

func TestAppOriginsGetCORSWithoutCredentials(t *testing.T) {
	a := &Service{AllowedOrigins: []string{"https://app.example"}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/api/login", nil)
	r.Header.Set("Origin", "tauri://localhost")
	a.cors(w, r)
	h := w.Header()
	if h.Get("Access-Control-Allow-Origin") != "tauri://localhost" || h.Get("Access-Control-Allow-Credentials") != "" ||
		!strings.Contains(h.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("headers %v", h)
	}
}

// http://tauri.localhost can be a local web page (*.localhost goes to the
// computer itself): with the cookie it is no desktop app, so the guard stays.
func TestAppOriginsWithACookieStayCrossSite(t *testing.T) {
	_, srv := setup(t)
	req, _ := http.NewRequest("POST", srv.URL+"/api/logout", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://tauri.localhost")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "x"})
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site with a cookie: %d, want 403", resp.StatusCode)
	}
}

func TestTheTokenComesBeforeACookie(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "old"})
	r.Header.Set("Authorization", "Bearer new")
	if (&Service{}).SessionKey(r) != tokenHash("new") {
		t.Fatal("the cookie won")
	}
}

// A body that never ends (a proxy without a body timeout passes it on)
// gets an answer after config.BodyTimeout: it does not hold its connection.
func TestStalledBodiesTimeOut(t *testing.T) {
	_, srv := setup(t)
	defer func(d time.Duration) { config.BodyTimeout = d }(config.BodyTimeout)
	config.BodyTimeout = 100 * time.Millisecond
	for _, path := range []string{"/api/login", "/api/accounts", "/api/password"} {
		body, pw := io.Pipe()
		defer pw.Close()
		req, _ := http.NewRequest("POST", srv.URL+path, body)
		req.ContentLength = 1000
		req.Header.Set("Content-Type", "application/json")
		done := make(chan struct{})
		go func() {
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
			close(done)
		}()
		io.WriteString(pw, `{"name":`)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the stalled body is still read", path)
		}
	}
}

func TestServerSaysIfGuestsNeedAServerPassword(t *testing.T) {
	for _, needed := range []bool{false, true} {
		a, srv := setup(t)
		a.ServerPassword = needed
		resp, err := http.Get(srv.URL + "/api/server")
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		if resp.StatusCode != 200 || v["password"] != needed {
			t.Fatalf("ServerPassword %v: %d %v", needed, resp.StatusCode, v)
		}
	}
}

func TestServerSaysItsNameAndLogo(t *testing.T) {
	a, srv := setup(t)
	get := func() map[string]any {
		resp, err := http.Get(srv.URL + "/api/server")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var v map[string]any
		json.NewDecoder(resp.Body).Decode(&v)
		return v
	}
	if v := get(); v["name"] != nil {
		t.Fatalf("no name known: %v", v)
	}
	a.ServerName = func() string { return "Our place" }
	if v := get(); v["name"] != "Our place" || v["logo"] != nil {
		t.Fatalf("got %v", v)
	}
	version, err := a.Store.SetLogo(map[int][]byte{32: []byte("png")})
	if err != nil {
		t.Fatal(err)
	}
	if v := get(); v["logo"] != version {
		t.Fatalf("with a logo %v", v)
	}
	// The apps send voice over UDP to the Mumble port, when there is one.
	if v := get(); v["udpPort"] != nil {
		t.Fatalf("no port: %v", v)
	}
	a.VoicePort = 64738
	if v := get(); v["udpPort"] != float64(64738) {
		t.Fatalf("port: %v", v)
	}
}

// Password guessing from another site, spread over its visitors'
// addresses, stops at a limit per account; only failures count, and logins
// on our own pages are not held up (no lockout from outside).
func TestLoginsPerAccountAreLimited(t *testing.T) {
	a, srv := setup(t)
	post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	post(t, srv, "/api/accounts", map[string]string{"name": "bob", "password": "longpassword"}, nil)
	a.TrustForwardedFor = true
	login := func(name, origin string, i int) int {
		req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(fmt.Sprintf(`{"name":%q,"password":"guess"}`, name)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if origin != "https://app.example" {
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.%d.%d", i/250, i%250+1))
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		return resp.StatusCode
	}
	const other = "https://other.example"
	status := 0
	for i := range 40 {
		if status = login("alice", other, i); status == http.StatusTooManyRequests {
			break
		}
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("40 guesses from 40 addresses: %d", status)
	}
	if got := login("bob", other, 100); got != http.StatusUnauthorized {
		t.Fatalf("bob: %d", got)
	}
	if got := login("alice", "https://app.example", 101); got != http.StatusUnauthorized {
		t.Fatalf("alice on our own page: %d", got)
	}
	// A name too long for any account is refused before it is counted.
	if got := login(strings.Repeat("x", 200), other, 102); got != http.StatusUnauthorized {
		t.Fatalf("a long name: %d", got)
	}
}

// A Mumble registration logs in without a session (the socket uses the
// name and password): not from another site, whose sockets need a token.
func TestMumbleRegistrationLoginsOnlyOnTheOwnSite(t *testing.T) {
	a, srv := setup(t)
	a.MumbleLogin = func(netip.Addr, string, string) (string, bool) { return "SuperUser", true }
	req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(`{"name":"superuser","password":"adminpw"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://other.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("from another site: %d, want 403", resp.StatusCode)
	}
}

// Parallel guesses are counted before the password work, not after.
func TestLoginsPerAccountHoldInParallel(t *testing.T) {
	a, srv := setup(t)
	post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	a.TrustForwardedFor = true
	var wg sync.WaitGroup
	var limited atomic.Int32
	for i := range 60 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(`{"name":"alice","password":"guess"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://other.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				limited.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := limited.Load(); n < 60-burst-1 {
		t.Fatalf("%d of 60 parallel guesses limited", n)
	}
}

// From another site, a name without a web account gets the same answer
// whatever the password (no hint that a Mumble password was right).
func TestMumbleRegistrationAnswerFromOtherSitesDoesNotDependOnThePassword(t *testing.T) {
	a, srv := setup(t)
	a.MumbleLogin = func(_ netip.Addr, _, pw string) (string, bool) { return "SuperUser", pw == "adminpw" }
	status := func(pw string) int {
		req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(fmt.Sprintf(`{"name":"superuser","password":%q}`, pw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://other.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		return resp.StatusCode
	}
	if right, wrong := status("adminpw"), status("nope"); right != wrong {
		t.Fatalf("right %d, wrong %d", right, wrong)
	}
}

// With HTTPS the cookie is __Host- (only this host can set it: a sibling
// subdomain cannot plant one; a plain-named one: see
// TestPlainCookiesMoveToHostOnly).
func TestHostOnlyCookiesOverHTTPS(t *testing.T) {
	a, srv := setup(t)
	a.TrustForwardedFor = true
	req, _ := http.NewRequest("POST", srv.URL+"/api/accounts", strings.NewReader(`{"name":"alice","password":"longpassword"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var c *http.Cookie
	for _, x := range resp.Cookies() {
		if x.Name == "__Host-"+CookieName {
			c = x
		}
	}
	if c == nil || !c.Secure || c.Path != "/" || c.Domain != "" {
		t.Fatalf("cookies %v", resp.Cookies())
	}
	for _, proto := range []string{"https", "wss"} {
		r := httptest.NewRequest("GET", "/ws", nil)
		r.Header.Set("X-Forwarded-Proto", proto)
		r.AddCookie(c)
		if _, ok := a.AccountFor(r); !ok {
			t.Fatalf("the HTTPS session did not authenticate over %s", proto)
		}
	}
}

// HTTPS in ALLOWED_ORIGINS is not proof of the request's transport.
// Forwarded headers affect cookies only when proxy trust is enabled.
func TestSessionCookieProxyTrust(t *testing.T) {
	for _, tc := range []struct {
		name, proto string
		trust       bool
	}{
		{"untrusted https header", "https", false},
		{"untrusted wss header", "wss", false},
		{"trusted ws header", "ws", true},
		{"plain http", "", false},
		{"trusted missing header", "", true},
		{"trusted http header", "http", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, srv := setup(t)
			a.TrustForwardedFor = tc.trust
			req, _ := http.NewRequest("POST", srv.URL+"/api/accounts", strings.NewReader(`{"name":"alice","password":"longpassword"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-Proto", tc.proto)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			c := sessionCookie(resp)
			if resp.StatusCode != http.StatusCreated || len(resp.Cookies()) != 1 || c == nil || c.Value == "" || c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
				t.Fatalf("status %d, cookies %v", resp.StatusCode, resp.Cookies())
			}
			r := httptest.NewRequest("GET", "/api/me", nil)
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			r.AddCookie(c)
			if _, ok := a.AccountFor(r); !ok {
				t.Fatal("the plain HTTP session cookie did not count")
			}
		})
	}
}

// Sessions from before the __Host- cookies keep counting with the plain
// name (until plainCookiesUntil: they end by then), and /api/me moves them
// to the new name. A session made later (one a sibling subdomain could
// plant now) does not count with it.
func TestPlainCookiesMoveToHostOnly(t *testing.T) {
	a, srv := setup(t)
	a.TrustForwardedFor = true
	a.Now = func() time.Time { return plainCookiesUntil.Add(-2 * time.Hour) }
	acc, _ := a.Store.CreateAccount(store.Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, time.Now(), "")
	a.Store.CreateSession(tokenHash("old"), acc.ID, plainCookiesUntil.Add(-time.Hour))
	a.Store.CreateSession(tokenHash("new"), acc.ID, plainCookiesUntil.Add(time.Hour))
	get := func(cookies ...*http.Cookie) *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+"/api/me", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	resp := get(&http.Cookie{Name: CookieName, Value: "old"})
	set := map[string]*http.Cookie{}
	for _, c := range resp.Cookies() {
		set[c.Name] = c
	}
	if resp.StatusCode != http.StatusOK || set["__Host-"+CookieName] == nil || set["__Host-"+CookieName].Value != "old" ||
		set[CookieName] == nil || set[CookieName].MaxAge >= 0 {
		t.Fatalf("an old session: %d %v", resp.StatusCode, resp.Cookies())
	}
	if resp := get(&http.Cookie{Name: CookieName, Value: "new"}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a newer session with the plain name: %d", resp.StatusCode)
	}
	// The __Host- cookie wins, also when its session ended.
	if resp := get(&http.Cookie{Name: "__Host-" + CookieName, Value: "gone"}, &http.Cookie{Name: CookieName, Value: "old"}); resp.StatusCode != http.StatusUnauthorized || len(resp.Cookies()) != 0 {
		t.Fatalf("both cookies: %d %v", resp.StatusCode, resp.Cookies())
	}
}

// A full table never drops a name that is still counting: a blocked
// account cannot be reset by many other names; new names wait instead.
func TestFailuresKeepActiveNamesWhenFull(t *testing.T) {
	var f failures
	now := time.Now()
	for range burst {
		f.take("alice", now)
	}
	if ok, full := f.take("alice", now); ok || full {
		t.Fatal("alice not blocked by name")
	}
	for i := range maxFailureNames {
		f.take(fmt.Sprintf("junk%d", i), now)
	}
	if ok, full := f.take("alice", now); ok || full {
		t.Fatal("alice reset by a full table, or blamed on it")
	}
	if ok, full := f.take("newcomer", now); ok || !full {
		t.Fatal("a new name got in while every name is counting")
	}
	// Later, names that are full again make room.
	if ok, _ := f.take("newcomer", now.Add(time.Hour)); !ok {
		t.Fatal("no room after an hour")
	}
}

// Names without a web account give their token back (also with Murmur):
// random names cannot fill the table.
func TestUnknownNamesFromOtherSitesDoNotFillTheTable(t *testing.T) {
	a, srv := setup(t)
	a.TrustForwardedFor = true
	for i := range 20 {
		req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(fmt.Sprintf(`{"name":"nobody%d","password":"guess"}`, i)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://other.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("nobody%d: %d", i, resp.StatusCode)
		}
	}
	a.failures.mu.Lock()
	defer a.failures.mu.Unlock()
	for _, b := range a.failures.m {
		if b.tokens < burst {
			t.Fatal("an unknown name kept its token")
		}
	}
}

// /api/me names the session by a short fingerprint (not its key): the
// same login gives the same one, another login of the account another.
func TestMeNamesTheSession(t *testing.T) {
	_, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	first := sessionCookie(resp)
	resp, _ = post(t, srv, "/api/login", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	second := sessionCookie(resp)
	me := func(c *http.Cookie) string {
		req, _ := http.NewRequest("GET", srv.URL+"/api/me", nil)
		req.AddCookie(c)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var v map[string]any
		json.NewDecoder(resp.Body).Decode(&v)
		s, _ := v["session"].(string)
		return s
	}
	a, b := me(first), me(second)
	if a == "" || len(a) != 16 || a != me(first) || a == b || strings.Contains(tokenHash(first.Value), a) {
		t.Fatalf("sessions %q %q", a, b)
	}
}

// A logout can name the session it means (a login screen that shows an
// older login): another one is not logged out.
func TestLogoutOfAnotherSessionIsRefused(t *testing.T) {
	a, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	key := (&Service{}).SessionKey(requestWith(c))
	resp, _ = post(t, srv, "/api/logout", map[string]string{"session": "0000000000000000"}, c)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("another session: %d, want 409", resp.StatusCode)
	}
	if _, ok := a.AccountFor(requestWith(c)); !ok {
		t.Fatal("the session ended")
	}
	if resp, _ = post(t, srv, "/api/logout", map[string]string{"session": sessionID(key)}, c); resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("its own session: %d", resp.StatusCode)
	}
	if _, ok := a.AccountFor(requestWith(c)); ok {
		t.Fatal("still logged in")
	}
}

// Without any session (already logged out) a named logout is no conflict.
func TestLogoutNamingASessionWithoutOne(t *testing.T) {
	_, srv := setup(t)
	if resp, _ := post(t, srv, "/api/logout", map[string]string{"session": "0123456789abcdef"}, nil); resp.StatusCode == http.StatusConflict {
		t.Fatal("409 without a session")
	}
}

// A logout can name the account it shows (a guest has no fingerprint):
// another account's login is not ended.
func TestLogoutOfAnotherAccountIsRefused(t *testing.T) {
	a, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "bob", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	resp, _ = post(t, srv, "/api/logout", map[string]string{"name": "guest1"}, c)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("another account: %d, want 409", resp.StatusCode)
	}
	if _, ok := a.AccountFor(requestWith(c)); !ok {
		t.Fatal("bob was logged out")
	}
	if resp, _ = post(t, srv, "/api/logout", map[string]string{"name": "Bob"}, c); resp.StatusCode == http.StatusConflict {
		t.Fatal("bob's own logout refused")
	}
}

// TokenEnded: a token that is no session; not a cookie, not a valid token.
func TestTokenEnded(t *testing.T) {
	a, srv := setup(t)
	req := func(h, v string) *http.Request {
		r := httptest.NewRequest("GET", "/ws", nil)
		r.Header.Set(h, v)
		return r
	}
	if !a.TokenEnded(req("Authorization", "Bearer dead")) || !a.TokenEnded(req("Sec-WebSocket-Protocol", "kallisti, "+SessionProtocol+"dead")) {
		t.Fatal("a dead token is not ended")
	}
	resp, v := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	_ = v
	c := sessionCookie(resp)
	if a.TokenEnded(req("Authorization", "Bearer "+c.Value)) {
		t.Fatal("a valid token counts as ended")
	}
	if a.TokenEnded(requestWith(&http.Cookie{Name: CookieName, Value: "dead"})) {
		t.Fatal("a cookie counts as a token")
	}
}
