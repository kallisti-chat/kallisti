package auth

import (
	"net/http"
	"testing"
	"time"

	"kallisti.chat/server/internal/store"
)

// An admin's recovery link (made in the sidecar) sets a new password once,
// ends the account's sessions and connections, and logs in.
func TestRecoveryLinkSetsAPassword(t *testing.T) {
	a, srv := setup(t)
	var ended []string
	a.EndPasswordLogins = func(acc store.Account) { ended = append(ended, acc.Name) }
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "erin", "password": "forgotten one"}, nil)
	old := sessionCookie(resp)
	acc, _ := a.Store.AccountByName("erin")
	a.Store.SetRecoveryLink(acc.ID, "the-token", time.Now().Add(time.Hour))

	if resp, v := post(t, srv, "/api/recover", map[string]string{"token": "the-token", "new": "short"}, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("short password: %d %v", resp.StatusCode, v)
	}
	resp, v := post(t, srv, "/api/recover", map[string]string{"token": "the-token", "new": "a new password"}, nil)
	if resp.StatusCode != http.StatusOK || v["name"] != "erin" || sessionCookie(resp) == nil {
		t.Fatalf("recover: %d %v", resp.StatusCode, v)
	}
	if _, ok := a.AccountFor(requestWith(old)); ok {
		t.Error("the old session still works")
	}
	if len(ended) != 1 {
		t.Errorf("connections ended for %v", ended)
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "erin", "password": "a new password"}, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("login with the new password: %d", resp.StatusCode)
	}
	if resp, _ := post(t, srv, "/api/recover", map[string]string{"token": "the-token", "new": "once more please"}, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second use: %d", resp.StatusCode)
	}
}

// With fediverse logins off, an account made with one cannot prove itself:
// no first password without the proof; an admin's recovery link helps.
func TestFirstPasswordWithFediOffNeedsRecovery(t *testing.T) {
	a, srv := setup(t)
	now := time.Now()
	acc, err := a.Store.CreateFediAccount(store.Account{Name: "fran", CertPEM: []byte("c"), KeyPEM: []byte("k")},
		store.FediIdentity{Host: "x.example", RemoteID: "1", Handle: "@fran@x.example"}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := a.newSession(acc)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: CookieName, Value: token}
	resp, v := post(t, srv, "/api/password", map[string]string{"new": "my first password"}, cookie)
	if resp.StatusCode != http.StatusForbidden || v["code"] == "reauth" {
		t.Fatalf("first password with fedi off: %d %v", resp.StatusCode, v)
	}
}
