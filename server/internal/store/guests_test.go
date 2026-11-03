package store

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestTemporaryAccounts(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	guest, err := s.CreateAccount(Account{Name: "visitor", CertPEM: []byte("c"), KeyPEM: []byte("k"), Temporary: true}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AccountByName("visitor"); !got.Temporary || got.PasswordHash != "" {
		t.Fatalf("%+v", got)
	}
	old, _ := s.CreateAccount(Account{Name: "gone", CertPEM: []byte("c"), KeyPEM: []byte("k"), Temporary: true}, now.Add(-48*time.Hour), "")
	kept, _ := s.CreateAccount(Account{Name: "kept", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now.Add(-48*time.Hour), "")
	// Seen lately: not idle.
	back, _ := s.CreateAccount(Account{Name: "back", CertPEM: []byte("c"), KeyPEM: []byte("k"), Temporary: true}, now.Add(-48*time.Hour), "")
	if err := s.TouchAccount(back.ID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	idle, err := s.IdleTemporaryAccounts(now.Add(-24 * time.Hour))
	if err != nil || len(idle) != 1 || idle[0].ID != old.ID {
		t.Fatalf("idle %+v %v", idle, err)
	}
	// A password makes it a normal account.
	if err := s.MakePermanent(guest.ID, "hash"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AccountByName("visitor"); got.Temporary || got.PasswordHash != "hash" {
		t.Fatalf("%+v", got)
	}
	// Only while idle and temporary.
	if deleted, _ := s.DeleteIdleTemporaryAccount(back.ID, now.Add(-24*time.Hour)); deleted {
		t.Fatal("deleted an account seen lately")
	}
	if deleted, _ := s.DeleteIdleTemporaryAccount(kept.ID, now); deleted {
		t.Fatal("deleted a normal account")
	}
	// Deleting takes the sessions too.
	s.CreateSession("tok", old.ID, now.Add(time.Hour))
	if err := s.DeleteAccount(old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountBySession("tok", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session left: %v", err)
	}
	if n, _ := s.TemporaryAccounts(); n != 1 { // back
		t.Fatalf("%d temporary accounts", n)
	}
	_ = kept
	if idle, _ := s.IdleTemporaryAccounts(now); !slices.ContainsFunc(idle, func(a Account) bool { return a.ID == back.ID }) {
		t.Fatalf("idle %+v", idle)
	}
}
