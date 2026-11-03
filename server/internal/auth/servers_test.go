package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/store"
)

// call sends a JSON request (nil body: none) from origin with the cookie.
func call(t *testing.T, srv *httptest.Server, method, path, origin string, body any, c *http.Cookie) (int, any) {
	t.Helper()
	var r *bytes.Reader = bytes.NewReader(nil)
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if c != nil {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v any
	json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

func TestTheAccountsServerList(t *testing.T) {
	_, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	const own = "https://app.example"
	list := func() []any {
		code, v := call(t, srv, "GET", "/api/servers", own, nil, c)
		if code != http.StatusOK {
			t.Fatalf("list: %d %v", code, v)
		}
		return v.(map[string]any)["servers"].([]any)
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("new account: %v", got)
	}
	if code, v := call(t, srv, "POST", "/api/servers", own, map[string]string{"origin": "https://chat.bob.org", "name": " Bob's "}, c); code != http.StatusOK {
		t.Fatalf("add: %d %v", code, v)
	}
	got := list()
	if len(got) != 1 || got[0].(map[string]any)["origin"] != "https://chat.bob.org" || got[0].(map[string]any)["name"] != "Bob's" {
		t.Fatalf("after add: %v", got)
	}
	if code, _ := call(t, srv, "DELETE", "/api/servers", own, map[string]string{"origin": "https://chat.bob.org"}, c); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("after remove: %v", got)
	}
}

func TestServerListChecks(t *testing.T) {
	_, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	const own = "https://app.example"
	add := func(origin string, c *http.Cookie, server, name string) int {
		code, _ := call(t, srv, "POST", "/api/servers", origin, map[string]string{"origin": server, "name": name}, c)
		return code
	}
	for _, bad := range []string{"chat.bob.org", "https://chat.bob.org/path", "https://u@chat.bob.org", "ftp://chat.bob.org", "https://Chat.Bob.org", "https://chat.bob.org/", "https://chat.bob.org:443", "https://chat.bob.org.", "https://:8443", "https://chat.bob.org:99999", own, ""} {
		if code := add(own, c, bad, "x"); code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", bad, code)
		}
	}
	for _, name := range []string{"", "  ", "a\nb", strings.Repeat("n", 129)} {
		if code := add(own, c, "https://chat.bob.org", name); code != http.StatusBadRequest {
			t.Errorf("name %q: %d, want 400", name, code)
		}
	}
	if code := add(own, nil, "https://chat.bob.org", "Bob"); code != http.StatusUnauthorized {
		t.Errorf("no login: %d", code)
	}
	if code := add("https://other.example", c, "https://chat.bob.org", "Bob"); code != http.StatusForbidden {
		t.Errorf("another site: %d", code)
	}
	for i := range maxAccountServers {
		if code := add(own, c, "https://s"+string(rune('a'+i%26))+string(rune('a'+i/26))+".example", "S"); code != http.StatusOK {
			t.Fatalf("server %d: %d", i, code)
		}
	}
	if code := add(own, c, "https://one-more.example", "S"); code != http.StatusConflict {
		t.Errorf("over the limit: %d", code)
	}
}

func TestGuestsHaveNoServerList(t *testing.T) {
	a, srv := setup(t)
	acc, _ := a.Store.CreateAccount(store.Account{Name: "guest", Temporary: true, CertPEM: []byte("c"), KeyPEM: []byte("k")}, time.Now(), "")
	a.Store.CreateSession(tokenHash("tok"), acc.ID, time.Now().Add(time.Hour))
	c := &http.Cookie{Name: CookieName, Value: "tok"}
	if code, _ := call(t, srv, "POST", "/api/servers", "https://app.example", map[string]string{"origin": "https://chat.bob.org", "name": "Bob"}, c); code != http.StatusForbidden {
		t.Fatalf("guest add: %d", code)
	}
}

// The apps use a token, from their own origin.
func TestTheAppsUseTheServerList(t *testing.T) {
	_, srv := setup(t)
	req, _ := http.NewRequest("POST", srv.URL+"/api/accounts", strings.NewReader(`{"name":"alice","password":"longpassword"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "tauri://localhost")
	req.Header.Set("X-Kallisti-Session", "token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Token string }
	json.NewDecoder(resp.Body).Decode(&v)
	resp.Body.Close()
	req, _ = http.NewRequest("GET", srv.URL+"/api/servers", nil)
	req.Header.Set("Origin", "tauri://localhost")
	req.Header.Set("Authorization", "Bearer "+v.Token)
	if resp, err = http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("list with a token: %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()
}

func TestTheRouterHasTheServerList(t *testing.T) {
	if !slices.Contains(Paths, "/api/servers") {
		t.Fatal("/api/servers is not in Paths")
	}
}

// A request may name the account it means (a stale window after another
// login in this browser): another one's list is not touched.
func TestServerListOfAnotherAccountIsRefused(t *testing.T) {
	_, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "bob", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	const own = "https://app.example"
	if code, _ := call(t, srv, "POST", "/api/servers", own, map[string]string{"origin": "https://r.example", "name": "x", "account": "Bob"}, c); code != http.StatusOK {
		t.Fatalf("own add: %d", code)
	}
	if code, _ := call(t, srv, "DELETE", "/api/servers", own, map[string]string{"origin": "https://r.example", "account": "alice"}, c); code != http.StatusConflict {
		t.Fatalf("another's remove: %d", code)
	}
	if code, _ := call(t, srv, "GET", "/api/servers?account=alice", own, nil, c); code != http.StatusConflict {
		t.Fatalf("another's list: %d", code)
	}
	if code, v := call(t, srv, "GET", "/api/servers?account=bob", own, nil, c); code != http.StatusOK || len(v.(map[string]any)["servers"].([]any)) != 1 {
		t.Fatalf("own list: %d %v", code, v)
	}
}

// Names as long as remote names (name@home) fit.
func TestServerListTakesLongNames(t *testing.T) {
	_, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	name := strings.Repeat("n", 100) + "@" + strings.Repeat("h", 23) + ".org"
	if code, v := call(t, srv, "POST", "/api/servers", "https://app.example", map[string]string{"origin": "https://chat.bob.org", "name": name}, c); code != http.StatusOK {
		t.Fatalf("a 128-character name: %d %v", code, v)
	}
}
