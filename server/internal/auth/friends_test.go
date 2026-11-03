package auth

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/store"
)

func TestTheAccountsFriends(t *testing.T) {
	_, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	const own = "https://app.example"
	list := func() []any {
		code, v := call(t, srv, "GET", "/api/friends", own, nil, c)
		if code != http.StatusOK {
			t.Fatalf("list: %d %v", code, v)
		}
		return v.(map[string]any)["friends"].([]any)
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("new account: %v", got)
	}
	// On another server, and on this one.
	for _, f := range []map[string]any{{"origin": "https://chat.bob.org", "user": 7, "name": " bob "}, {"origin": own, "user": 3, "name": "carol"}} {
		if code, v := call(t, srv, "POST", "/api/friends", own, f, c); code != http.StatusOK {
			t.Fatalf("add %v: %d %v", f, code, v)
		}
	}
	got := list()
	if len(got) != 2 || got[0].(map[string]any)["name"] != "bob" || got[0].(map[string]any)["user"] != 7.0 || got[1].(map[string]any)["origin"] != own {
		t.Fatalf("after add: %v", got)
	}
	if code, _ := call(t, srv, "DELETE", "/api/friends", own, map[string]any{"origin": "https://chat.bob.org", "user": 7}, c); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if got := list(); len(got) != 1 {
		t.Fatalf("after remove: %v", got)
	}
}

func TestFriendsChecks(t *testing.T) {
	_, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	c := sessionCookie(resp)
	const own = "https://app.example"
	add := func(origin string, c *http.Cookie, server string, user int, name string) int {
		code, _ := call(t, srv, "POST", "/api/friends", origin, map[string]any{"origin": server, "user": user, "name": name}, c)
		return code
	}
	for _, bad := range []string{"chat.bob.org", "https://chat.bob.org/path", "https://Chat.Bob.org", "https://chat.bob.org:443", ""} {
		if code := add(own, c, bad, 1, "x"); code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", bad, code)
		}
	}
	for _, name := range []string{"", "a\nb", strings.Repeat("n", 129)} {
		if code := add(own, c, "https://chat.bob.org", 1, name); code != http.StatusBadRequest {
			t.Errorf("name %q: %d, want 400", name, code)
		}
	}
	if code := add(own, c, "https://chat.bob.org", -1, "bob"); code != http.StatusBadRequest {
		t.Errorf("negative user: %d", code)
	}
	// No user id at all: not user 0.
	for _, method := range []string{"POST", "DELETE"} {
		if code, _ := call(t, srv, method, "/api/friends", own, map[string]any{"origin": "https://chat.bob.org", "name": "bob"}, c); code != http.StatusBadRequest {
			t.Errorf("%s without a user id: %d", method, code)
		}
	}
	if code := add(own, nil, "https://chat.bob.org", 1, "bob"); code != http.StatusUnauthorized {
		t.Errorf("no login: %d", code)
	}
	if code := add("https://other.example", c, "https://chat.bob.org", 1, "bob"); code != http.StatusForbidden {
		t.Errorf("another site: %d", code)
	}
	for i := range maxFriends {
		if code := add(own, c, "https://chat.bob.org", i+1, "f"); code != http.StatusOK {
			t.Fatalf("friend %d: %d", i, code)
		}
	}
	if code := add(own, c, "https://chat.bob.org", maxFriends+1, "f"); code != http.StatusConflict {
		t.Errorf("over the limit: %d", code)
	}
}

func TestGuestsHaveNoFriendsList(t *testing.T) {
	a, srv := setup(t)
	acc, _ := a.Store.CreateAccount(store.Account{Name: "guest", Temporary: true, CertPEM: []byte("c"), KeyPEM: []byte("k")}, time.Now(), "")
	a.Store.CreateSession(tokenHash("tok"), acc.ID, time.Now().Add(time.Hour))
	c := &http.Cookie{Name: CookieName, Value: "tok"}
	if code, _ := call(t, srv, "POST", "/api/friends", "https://app.example", map[string]any{"origin": "https://chat.bob.org", "user": 1, "name": "bob"}, c); code != http.StatusForbidden {
		t.Fatalf("guest add: %d", code)
	}
}

func TestTheRouterHasTheFriendsList(t *testing.T) {
	if !slices.Contains(Paths, "/api/friends") {
		t.Fatal("/api/friends is not in Paths")
	}
}
