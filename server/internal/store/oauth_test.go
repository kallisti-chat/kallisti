package store

import (
	"errors"
	"testing"
	"time"
)

func TestOAuthProvider(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	acc, _ := s.CreateAccount(Account{Name: "lain", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	const redirect = "https://b.example/api/fedi/callback"
	// Consent per redirect URI.
	if ok, _ := s.HasOAuthGrant(acc.ID, redirect); ok {
		t.Fatal("granted before consent")
	}
	if err := s.GrantOAuth(acc.ID, redirect, now); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.HasOAuthGrant(acc.ID, redirect); !ok {
		t.Fatal("no grant")
	}
	if ok, _ := s.HasOAuthGrant(acc.ID, "https://b.example/other"); ok {
		t.Fatal("another path of the site is granted")
	}
	// Tokens by hash, until they expire.
	if err := s.CreateOAuthToken("th", acc.ID, "cid", "read:accounts", now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, scopes, err := s.AccountByOAuthToken("th", now)
	if err != nil || got.ID != acc.ID || scopes != "read:accounts" {
		t.Fatalf("token: %+v %q %v", got, scopes, err)
	}
	if _, _, err := s.AccountByOAuthToken("th", now.Add(11*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token: %v", err)
	}
}

func TestOAuthGrantsListAndRevoke(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	acc, _ := s.CreateAccount(Account{Name: "lain", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	other, _ := s.CreateAccount(Account{Name: "bob", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	const b, c = "https://b.example/api/fedi/callback", "https://c.example/api/fedi/callback"
	s.GrantOAuth(acc.ID, b, now)
	s.GrantOAuth(acc.ID, c, now.Add(time.Minute))
	s.GrantOAuth(other.ID, b, now)
	s.CreateOAuthToken("tb", acc.ID, "cid-b", "read:accounts", now.Add(10*time.Minute))
	s.CreateOAuthToken("tc", acc.ID, "cid-c", "read:accounts", now.Add(10*time.Minute))
	s.CreateOAuthToken("ob", other.ID, "cid-b", "read:accounts", now.Add(10*time.Minute))
	got, err := s.OAuthGrants(acc.ID)
	if err != nil || len(got) != 2 || got[0].Redirect != c || got[1].Redirect != b || !got[1].CreatedAt.Equal(now) {
		t.Fatalf("grants, newest first: %+v %v", got, err)
	}
	// Revoked: the grant and its tokens; not other sites', not others'.
	if err := s.RevokeOAuth(acc.ID, b, "cid-b"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.HasOAuthGrant(acc.ID, b); ok {
		t.Fatal("still granted")
	}
	if _, _, err := s.AccountByOAuthToken("tb", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("its token: %v", err)
	}
	if _, _, err := s.AccountByOAuthToken("tc", now); err != nil {
		t.Fatalf("another site's token: %v", err)
	}
	if ok, _ := s.HasOAuthGrant(other.ID, b); !ok {
		t.Fatal("another account's grant went")
	}
	if _, _, err := s.AccountByOAuthToken("ob", now); err != nil {
		t.Fatalf("another account's token: %v", err)
	}
}
