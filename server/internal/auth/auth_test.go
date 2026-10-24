package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
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
		{"name": "x", "password": "longpassword"},                     // too short
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
			if match(acc.ID, SessionKey(requestWith(c))) != revoked {
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

func TestExpiredSessionsAreRevoked(t *testing.T) {
	a, srv := setup(t)
	now := time.Now()
	a.Now = func() time.Time { return now }
	var match func(int64, string) bool
	a.Revoke = func(m func(int64, string) bool) { match = m }
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "hank", "password": "longpassword"}, nil)
	old := SessionKey(requestWith(sessionCookie(resp)))
	acc, _ := a.Store.AccountBySession(old, now)
	now = now.Add(sessionLifetime / 2)
	resp, _ = post(t, srv, "/api/login", map[string]string{"name": "hank", "password": "longpassword"}, nil)
	current := SessionKey(requestWith(sessionCookie(resp)))
	now = now.Add(sessionLifetime / 2)
	if err := a.DeleteExpiredSessions(); err != nil {
		t.Fatal(err)
	}
	if match == nil || !match(acc.ID, old) || match(acc.ID, current) {
		t.Fatal("want only the expired session revoked")
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
	if match == nil || !match(0, SessionKey(requestWith(bob))) {
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
	if SessionKey(ws) != tokenHash(token) {
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
	if SessionKey(r) != tokenHash("new") {
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

func TestNoRegistration(t *testing.T) {
	a, srv := setup(t)
	a.GuestAccounts = true
	post(t, srv, "/api/accounts", map[string]string{"name": "early", "password": "longpassword"}, nil)
	a.NoRegistration = true
	if resp, v := post(t, srv, "/api/accounts", map[string]string{"name": "late", "password": "longpassword"}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("create: %d %v", resp.StatusCode, v)
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "early", "password": "longpassword"}, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	// Guests still join, but cannot keep the account.
	resp, _ := post(t, srv, "/api/guest", map[string]string{"name": "visitor"}, nil)
	c := sessionCookie(resp)
	if resp.StatusCode != http.StatusCreated || c == nil {
		t.Fatalf("guest: %d", resp.StatusCode)
	}
	if resp, _ := post(t, srv, "/api/password", map[string]string{"new": "a good password"}, c); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("keep guest account: %d", resp.StatusCode)
	}
	if acc, _ := a.AccountFor(requestWith(c)); !acc.Temporary {
		t.Fatal("the guest account was kept")
	}
	get, err := http.Get(srv.URL + "/api/server")
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	var v map[string]any
	json.NewDecoder(get.Body).Decode(&v)
	if v["registration"] != false {
		t.Fatalf("server: %v", v)
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
