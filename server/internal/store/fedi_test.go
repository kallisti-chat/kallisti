package store

import (
	"errors"
	"testing"
	"time"
)

func TestFediApps(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	if _, err := s.FediApp("social.example", "https://k.example/cb"); err != ErrNotFound {
		t.Fatalf("before one was saved: %v", err)
	}
	app := FediApp{Host: "social.example", RedirectURI: "https://k.example/cb", Scopes: "read:accounts profile", ClientID: "id", ClientSecret: "secret"}
	if err := s.SaveFediApp(app, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.FediApp("social.example", "https://k.example/cb")
	if err != nil || got.ClientID != "id" || got.ClientSecret != "secret" || got.Scopes != app.Scopes || got.Used || !got.CreatedAt.Equal(now) {
		t.Fatalf("saved app: %+v %v", got, err)
	}
	// An app that got a login is kept (instances may drop unused ones).
	if err := s.MarkFediAppUsed("social.example"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FediApp("social.example", "https://k.example/cb"); !got.Used {
		t.Fatalf("used: %+v", got)
	}
	// A new public address needs a new app.
	if _, err := s.FediApp("social.example", "https://other.example/cb"); err != ErrNotFound {
		t.Fatalf("other redirect: %v", err)
	}
	app.ClientID, app.RedirectURI = "id2", "https://other.example/cb"
	s.SaveFediApp(app, now)
	if got, _ := s.FediApp("social.example", "https://other.example/cb"); got.ClientID != "id2" {
		t.Fatalf("replaced app: %+v", got)
	}
}

func TestFediIdentities(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	id := FediIdentity{Host: "social.example", RemoteID: "42", Handle: "@alice@social.example"}

	// A new account with its identity, in one step.
	acc, err := s.CreateFediAccount(Account{Name: "alice", CertPEM: []byte("c"), KeyPEM: []byte("k")}, id, now, "")
	if err != nil || acc.ID == 0 || acc.PasswordHash != "" {
		t.Fatalf("create: %+v %v", acc, err)
	}
	got, ident, err := s.AccountByFedi("social.example", "42")
	if err != nil || got.ID != acc.ID || ident.Handle != "@alice@social.example" || ident.Public {
		t.Fatalf("by identity: %+v %+v %v", got, ident, err)
	}
	if _, _, err := s.AccountByFedi("social.example", "43"); err != ErrNotFound {
		t.Fatalf("unknown identity: %v", err)
	}
	// The identity belongs to one account: no second one with it, and the
	// name is not taken by the failed try.
	if _, err := s.CreateFediAccount(Account{Name: "alice2", CertPEM: []byte("c"), KeyPEM: []byte("k")}, id, now, ""); !errors.Is(err, ErrFediTaken) {
		t.Fatalf("second account: %v", err)
	}
	if _, err := s.AccountByName("alice2"); err != ErrNotFound {
		t.Fatalf("half-made account: %v", err)
	}
	if _, err := s.CreateFediAccount(Account{Name: "ALICE", CertPEM: []byte("c"), KeyPEM: []byte("k")}, FediIdentity{Host: "x.example", RemoteID: "1", Handle: "@a@x.example"}, now, ""); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("taken name: %v", err)
	}

	// The last way to log in stays.
	if err := s.UnlinkFedi(acc.ID); !errors.Is(err, ErrLastLogin) {
		t.Fatalf("unlink the only login: %v", err)
	}
	if err := s.ChangePassword(acc.ID, "", "", "hash", now); err != ErrNotFound {
		t.Fatalf("password without a session: %v", err) // see ChangePassword
	}
	s.CreateSession("tok", acc.ID, now.Add(time.Hour))
	if err := s.ChangePassword(acc.ID, "tok", "", "hash", now); err != nil {
		t.Fatalf("first password: %v", err)
	}
	if err := s.UnlinkFedi(acc.ID); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if err := s.UnlinkFedi(acc.ID); err != ErrNotFound {
		t.Fatalf("unlink twice: %v", err)
	}

	// Linking: a guest account becomes permanent; one identity per account.
	guest, _ := s.CreateAccount(Account{Name: "guest", Temporary: true, CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	later := now.Add(time.Hour)
	s.CreateSession("guest-session", guest.ID, later)
	s.CreateSession("acc-session", acc.ID, later)
	// Only while the session that started it lasts (a logout everywhere
	// meanwhile ends it).
	if err := s.LinkFedi(guest.ID, "gone", id, now); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("link without the session: %v", err)
	}
	if err := s.LinkFedi(guest.ID, "acc-session", id, now); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("link with another account's session: %v", err)
	}
	if err := s.LinkFedi(guest.ID, "guest-session", id, now); err != nil {
		t.Fatalf("link: %v", err)
	}
	if g, _ := s.AccountByName("guest"); g.Temporary {
		t.Fatal("a linked guest account stays temporary")
	}
	if err := s.LinkFedi(acc.ID, "acc-session", id, now); !errors.Is(err, ErrFediTaken) {
		t.Fatalf("identity of another account: %v", err)
	}
	if err := s.LinkFedi(guest.ID, "guest-session", FediIdentity{Host: "x.example", RemoteID: "1", Handle: "@g@x.example"}, now); !errors.Is(err, ErrFediTaken) {
		t.Fatalf("second identity: %v", err)
	}
	if ident, err := s.FediIdentityOf(guest.ID); err != nil || ident.RemoteID != "42" {
		t.Fatalf("identity of: %+v %v", ident, err)
	}

	// The handle on the profile, opt-in; it follows renames on the instance.
	if h, err := s.PublicFediHandle("GUEST"); err != nil || h != "" {
		t.Fatalf("handle before opting in: %q %v", h, err)
	}
	if err := s.SetFediPublic(guest.ID, true); err != nil {
		t.Fatal(err)
	}
	s.UpdateFediHandle("social.example", "42", "@alice2@social.example")
	if h, _ := s.PublicFediHandle("guest"); h != "@alice2@social.example" {
		t.Fatalf("public handle: %q", h)
	}
	if err := s.SetFediPublic(acc.ID, true); err != ErrNotFound {
		t.Fatalf("public without identity: %v", err)
	}

	// Identities go with their account.
	s.DeleteAccount(guest.ID)
	if _, _, err := s.AccountByFedi("social.example", "42"); err != ErrNotFound {
		t.Fatalf("after delete: %v", err)
	}
}

func TestFediOnlyAccounts(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	only, err := s.CreateFediAccount(Account{Name: "ann@a.example", CertPEM: []byte("c"), KeyPEM: []byte("k")}, FediIdentity{Host: "a.example", RemoteID: "1", Handle: "@ann@a.example"}, now, "")

	if err != nil {
		t.Fatal(err)
	}
	// With a password: it logs in without its remote account too.
	both, err := s.CreateAccount(Account{Name: "bob", PasswordHash: "hash", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("h", both.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkFedi(both.ID, "h", FediIdentity{Host: "b.example", RemoteID: "2", Handle: "@bob@b.example"}, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.FediOnlyAccounts()
	if err != nil || len(got) != 1 || got[0].AccountID != only.ID || got[0].Name != "ann@a.example" || got[0].Identity.Handle != "@ann@a.example" || got[0].Identity.Host != "a.example" {
		t.Fatalf("fedi-only accounts: %+v %v", got, err)
	}
}
