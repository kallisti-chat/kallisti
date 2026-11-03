package auth

import (
	"net/http"
	"testing"
	"time"

	"kallisti.chat/server/internal/fedi/fedifake"
	"kallisti.chat/server/internal/store"
)

func TestInviteOnlyAccountsGuestsAndExistingLogins(t *testing.T) {
	a, srv := setup(t)
	a.GuestAccounts = true
	creds := map[string]string{"name": "alice", "password": "longpassword"}
	resp, _ := post(t, srv, "/api/accounts", creds, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatal(resp.StatusCode)
	}
	cookie := sessionCookie(resp)
	guest, _ := post(t, srv, "/api/guest", map[string]string{"name": "oldguest"}, nil)
	if guest.StatusCode != http.StatusCreated {
		t.Fatal(guest.StatusCode)
	}
	guestCookie := sessionCookie(guest)
	if err := a.Store.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/accounts", "/api/guest"} {
		resp, v := post(t, srv, path, map[string]string{"name": "newuser", "password": "longpassword", "invite": "wrong"}, nil)
		if resp.StatusCode != http.StatusForbidden || v["code"] != "invite_required" || sessionCookie(resp) != nil {
			t.Fatalf("%s: %d %v", path, resp.StatusCode, v)
		}
	}
	for _, c := range []*http.Cookie{cookie, guestCookie} {
		if _, ok := a.AccountFor(requestWith(c)); !ok {
			t.Fatal("existing session lost access")
		}
	}
	if resp, _ := post(t, srv, "/api/login", creds, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("existing login: %d", resp.StatusCode)
	}
	if err := a.Store.AddRegistrationInvite("valid", store.RegistrationInvite{Creator: "admin", Created: a.Now().UnixMilli(), MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	resp, v := post(t, srv, "/api/accounts", map[string]string{"name": "invited", "password": "longpassword", "invite": "valid"}, nil)
	if resp.StatusCode != http.StatusCreated || sessionCookie(resp) == nil {
		t.Fatalf("invited: %d %v", resp.StatusCode, v)
	}
	if resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "reuse", "password": "longpassword", "invite": "valid"}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("reuse: %d", resp.StatusCode)
	}
	if err := a.Store.SetInviteOnly(false); err != nil {
		t.Fatal(err)
	}
	if resp, _ := post(t, srv, "/api/guest", map[string]string{"name": "newguest"}, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("reopened: %d", resp.StatusCode)
	}
}

func TestInviteOnlyFederatedSignupAndExistingLogin(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": inst}, "Alice"), home+"?fedi=register")
	// Policy can change while the browser is at its identity provider.
	if err := a.Store.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	if resp, _ := b.do("POST", "/api/fedi/signup", map[string]string{"name": "alice"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("uninvited: %d", resp.StatusCode)
	}
	if err := a.Store.AddRegistrationInvite("fedi", store.RegistrationInvite{Creator: "admin", Created: time.Now().UnixMilli(), MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	resp, v := b.do("POST", "/api/fedi/signup", map[string]string{"name": "alice", "invite": "fedi"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("invited: %d %v", resp.StatusCode, v)
	}
	other := newBrowser(t, srv)
	wantRedirect(t, other.fediFlow(f, map[string]any{"instance": inst}, "Alice"), home)
	if _, v := other.do("GET", "/api/me", nil); v["name"] != "alice" {
		t.Fatalf("existing fedi login: %v", v)
	}
	unknown := newBrowser(t, srv)
	wantRedirect(t, unknown.fediFlow(f, map[string]any{"instance": inst}, "Bob"), home+"?fedi=register")
	if resp, _ := unknown.do("POST", "/api/fedi/signup", map[string]string{"name": "bob", "invite": "fedi"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("reused invitation: %d", resp.StatusCode)
	}
}

func TestRegistrationFailureClosesAdmission(t *testing.T) {
	a, srv := setup(t)
	a.GuestAccounts = true
	a.Store.Close()
	for _, path := range []string{"/api/accounts", "/api/guest"} {
		resp, _ := post(t, srv, path, map[string]string{"name": "new", "password": "longpassword"}, nil)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
}

func TestInviteOnlyFederatedSignupPreservesHandoffConsent(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	if err := a.Store.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.AddRegistrationInvite("handoff", store.RegistrationInvite{Creator: "admin", Created: a.Now().UnixMilli(), MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, srv)
	to := "https://home.example"
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": inst, "handoff": true, "to": to}, "Alice"), home+"?fedi=register")
	if resp, _ := b.do("POST", "/api/fedi/signup", map[string]string{"name": "alice"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("uninvited: %d", resp.StatusCode)
	}
	if resp, v := b.do("POST", "/api/fedi/signup", map[string]string{"name": "alice", "invite": "handoff"}); resp.StatusCode != http.StatusCreated || v["handoff"] != true {
		t.Fatalf("invited: %d %v", resp.StatusCode, v)
	}
	resp, v := b.do("POST", "/api/fedi/handoff", map[string]any{})
	if resp.StatusCode != http.StatusOK || v["ask"] == nil || v["ask"] == "" || v["origin"] != to || v["token"] != nil {
		t.Fatalf("handoff requires consent: %d %v", resp.StatusCode, v)
	}
	ask := v["ask"]
	if resp, v := b.do("POST", "/api/fedi/handoff", map[string]any{"allow": "wrong"}); resp.StatusCode != http.StatusForbidden || v["token"] != nil {
		t.Fatalf("wrong consent: %d %v", resp.StatusCode, v)
	}
	resp, v = b.do("POST", "/api/fedi/handoff", map[string]any{"allow": ask})
	if resp.StatusCode != http.StatusOK || v["name"] != "alice" || v["origin"] != to || v["token"] == nil || v["token"] == "" {
		t.Fatalf("approved handoff: %d %v", resp.StatusCode, v)
	}
}

func TestRegistrationInviteDoesNotBypassBrowserCheck(t *testing.T) {
	a, srv := setup(t)
	a.Captcha = func() string { return "normal" }
	if err := a.Store.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.AddRegistrationInvite("valid", store.RegistrationInvite{Creator: "admin", Created: a.Now().UnixMilli(), MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	body := map[string]string{"name": "alice", "password": "longpassword", "invite": "valid"}
	if resp, _ := post(t, srv, "/api/accounts", body, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("without browser check: %d", resp.StatusCode)
	}
	challenge, bits := getChallenge(t, srv.URL)
	body["challenge"], body["nonce"] = challenge, solve(challenge, bits)
	if resp, v := post(t, srv, "/api/accounts", body, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("with browser check: %d %v", resp.StatusCode, v)
	}
}

func TestInviteOnlyExistingAccountQRLogin(t *testing.T) {
	a, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "alice", "password": "longpassword"}, nil)
	alice := sessionCookie(resp)
	if err := a.Store.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	_, offer := post(t, srv, "/api/qr/start", map[string]string{"device": "computer"}, alice)
	_, scan := post(t, srv, "/api/qr/scan", map[string]any{"token": offer["code"], "device": "phone"}, nil)
	resp, _ = post(t, srv, "/api/qr/answer", map[string]any{"secret": offer["owner"], "yes": true}, alice)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("approval: %d", resp.StatusCode)
	}
	resp, answer := post(t, srv, "/api/qr/wait", map[string]any{"secret": scan["claim"]}, nil)
	if resp.StatusCode != http.StatusOK || answer["state"] != "done" || answer["name"] != "alice" {
		t.Fatalf("QR login: %d %v", resp.StatusCode, answer)
	}
	if acc, ok := a.AccountFor(requestWith(sessionCookie(resp))); !ok || acc.Name != "alice" {
		t.Fatal("no QR session")
	}
}

func TestInviteOnlyExistingGuestCanBecomePermanent(t *testing.T) {
	a, srv := setup(t)
	a.GuestAccounts = true
	resp, _ := post(t, srv, "/api/guest", map[string]string{"name": "existingguest"}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatal(resp.StatusCode)
	}
	cookie := sessionCookie(resp)
	if err := a.Store.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	resp, v := post(t, srv, "/api/password", map[string]string{"new": "longpassword"}, cookie)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("make permanent: %d %v", resp.StatusCode, v)
	}
	if acc, ok := a.AccountFor(requestWith(cookie)); !ok || acc.Temporary {
		t.Fatal("existing session did not become permanent")
	}
	resp, _ = post(t, srv, "/api/login", map[string]string{"name": "existingguest", "password": "longpassword"}, nil)
	if resp.StatusCode != http.StatusOK || sessionCookie(resp) == nil {
		t.Fatalf("permanent account login: %d", resp.StatusCode)
	}
	if resp, _ := post(t, srv, "/api/guest", map[string]string{"name": "newguest"}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("new guest: %d", resp.StatusCode)
	}
}
