package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/store"
)

func streamKeyRequest(t *testing.T, srvURL, method string, c *http.Cookie) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srvURL+"/api/stream-key", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
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

func TestStreamKeys(t *testing.T) {
	a, srv := setup(t)
	a.StreamKeys = true
	var changed []string
	a.StreamKeyChanged = func(name string) { changed = append(changed, name) }
	if resp, _ := streamKeyRequest(t, srv.URL, "POST", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a key without login: %d", resp.StatusCode)
	}
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "Alice", "password": "correct horse"}, nil)
	c := sessionCookie(resp)
	if _, v := streamKeyRequest(t, srv.URL, "GET", c); v["exists"] != false || v["enabled"] != true {
		t.Fatalf("before: %v", v)
	}
	resp, v := streamKeyRequest(t, srv.URL, "POST", c)
	key, _ := v["key"].(string)
	if resp.StatusCode != http.StatusOK || len(key) < 40 {
		t.Fatalf("new key: %d %v", resp.StatusCode, v)
	}
	if _, v := streamKeyRequest(t, srv.URL, "GET", c); v["exists"] != true || v["key"] != nil {
		t.Fatalf("after: %v", v)
	}
	// Stored hashed: the key itself is nowhere in the database.
	var stored string
	a.Store.DB().QueryRow(`SELECT key_hash FROM stream_keys`).Scan(&stored)
	if stored == "" || stored == key || stored != tokenHash(key) {
		t.Fatalf("stored %q", stored)
	}
	if acc, ok := a.StreamKeyAccount(key); !ok || acc.Name != "Alice" {
		t.Fatalf("account of the key: %+v %v", acc, ok)
	}
	for _, wrong := range []string{"", "x", key + "x", strings.ToUpper(key)} {
		if _, ok := a.StreamKeyAccount(wrong); ok {
			t.Fatalf("wrong key %q works", wrong)
		}
	}
	// A new key: the old one stops working.
	_, v = streamKeyRequest(t, srv.URL, "POST", c)
	if _, ok := a.StreamKeyAccount(key); ok || v["key"] == key {
		t.Fatal("the old key still works")
	}
	if _, ok := a.StreamKeyAccount(v["key"].(string)); !ok {
		t.Fatal("the new key does not work")
	}
	if resp, _ := streamKeyRequest(t, srv.URL, "DELETE", c); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if _, ok := a.StreamKeyAccount(v["key"].(string)); ok {
		t.Fatal("a deleted key works")
	}
	if strings.Join(changed, ",") != "Alice,Alice,Alice" {
		t.Fatalf("changes %v", changed)
	}
}

// A stream key is a credential like a session: a new password and "log out
// everywhere" end it (and its stream).
func TestStreamKeysEndWithTheSessions(t *testing.T) {
	a, srv := setup(t)
	a.StreamKeys = true
	var changed []string
	a.StreamKeyChanged = func(name string) { changed = append(changed, name) }
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "Alice", "password": "correct horse"}, nil)
	c := sessionCookie(resp)
	_, v := streamKeyRequest(t, srv.URL, "POST", c)
	if resp, _ := post(t, srv, "/api/password", map[string]string{"old": "correct horse", "new": "battery staple"}, c); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("password: %d", resp.StatusCode)
	}
	if _, ok := a.StreamKeyAccount(v["key"].(string)); ok {
		t.Fatal("the key works after a new password")
	}
	_, v = streamKeyRequest(t, srv.URL, "POST", c)
	if resp, _ := post(t, srv, "/api/logout-all", map[string]string{}, c); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout everywhere: %d", resp.StatusCode)
	}
	if _, ok := a.StreamKeyAccount(v["key"].(string)); ok {
		t.Fatal("the key works after logging out everywhere")
	}
	if strings.Join(changed, ",") != "Alice,Alice,Alice,Alice" {
		t.Fatalf("changes %v", changed)
	}
}

func TestStreamKeysOff(t *testing.T) {
	_, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "Alice", "password": "correct horse"}, nil)
	c := sessionCookie(resp)
	if _, v := streamKeyRequest(t, srv.URL, "GET", c); v["enabled"] != false {
		t.Fatalf("off: %v", v)
	}
	if resp, _ := streamKeyRequest(t, srv.URL, "POST", c); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a key while off: %d", resp.StatusCode)
	}
}

func TestGuestsHaveNoStreamKey(t *testing.T) {
	a, srv := setup(t)
	a.StreamKeys = true
	acc, _ := a.Store.CreateAccount(store.Account{Name: "guest", Temporary: true, CertPEM: []byte("c"), KeyPEM: []byte("k")}, time.Now(), "")
	a.Store.CreateSession(tokenHash("tok"), acc.ID, time.Now().Add(time.Hour))
	if resp, _ := streamKeyRequest(t, srv.URL, "POST", &http.Cookie{Name: CookieName, Value: "tok"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a guest's key: %d", resp.StatusCode)
	}
	// Nor does a key made before work for a guest account.
	a.Store.SetStreamKey(acc.ID, tokenHash("old"), time.Now())
	if _, ok := a.StreamKeyAccount("old"); ok {
		t.Fatal("a guest's key works")
	}
}
