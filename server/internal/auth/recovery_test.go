package auth

import (
	"database/sql"
	"errors"
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

// An imported Mumble user's link: it makes the account with the
// registration's name (moved to the account's certificate) and logs in.
func TestClaimLinkMakesTheAccount(t *testing.T) {
	a, srv := setup(t)
	var moved store.ClaimLink
	var movedTo store.Account
	a.MoveRegistration = func(_ *sql.Tx, l store.ClaimLink, acc store.Account) error {
		moved, movedTo = l, acc
		return nil
	}
	link := store.ClaimLink{User: 9, Name: "Feld", CertHash: "old"}
	a.Store.SetClaimLink(link, "the-token", time.Now().Add(time.Hour))

	resp, v := post(t, srv, "/api/recover", map[string]string{"token": "the-token", "new": "a new password"}, nil)
	if resp.StatusCode != http.StatusOK || v["name"] != "Feld" || sessionCookie(resp) == nil {
		t.Fatalf("claim: %d %v", resp.StatusCode, v)
	}
	acc, err := a.Store.AccountByName("feld")
	if err != nil || moved != link || movedTo.ID != acc.ID || len(acc.CertPEM) == 0 {
		t.Fatalf("account %+v %v, moved %+v to %d", acc, err, moved, movedTo.ID)
	}
	if resp, _ := post(t, srv, "/api/login", map[string]string{"name": "Feld", "password": "a new password"}, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("login with the new password: %d", resp.StatusCode)
	}
	if resp, _ := post(t, srv, "/api/recover", map[string]string{"token": "the-token", "new": "once more please"}, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second use: %d", resp.StatusCode)
	}
}

// The registration changed since the link was made: no account.
func TestClaimLinkForAChangedRegistration(t *testing.T) {
	a, srv := setup(t)
	a.MoveRegistration = func(*sql.Tx, store.ClaimLink, store.Account) error { return store.ErrNotFound }
	a.Store.SetClaimLink(store.ClaimLink{User: 9, Name: "feld", CertHash: "old"}, "the-token", time.Now().Add(time.Hour))
	if resp, _ := post(t, srv, "/api/recover", map[string]string{"token": "the-token", "new": "a new password"}, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("changed registration: %d", resp.StatusCode)
	}
	if _, err := a.Store.AccountByName("feld"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("account: %v", err)
	}
}
