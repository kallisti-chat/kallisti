package store

import (
	"testing"
	"time"
)

func TestStreamKeys(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	a, _ := s.CreateAccount(Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	b, _ := s.CreateAccount(Account{Name: "bob", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if has, err := s.HasStreamKey(a.ID); err != nil || has {
		t.Fatalf("a key before one was made: %v %v", has, err)
	}
	if err := s.SetStreamKey(a.ID, "hash1", now); err != nil {
		t.Fatal(err)
	}
	s.SetStreamKey(b.ID, "hash2", now)
	if got, err := s.AccountByStreamKey("hash1"); err != nil || got.ID != a.ID {
		t.Fatalf("by key: %+v %v", got, err)
	}
	// A new key replaces the old one.
	if err := s.SetStreamKey(a.ID, "hash3", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountByStreamKey("hash1"); err != ErrNotFound {
		t.Fatalf("old key: %v", err)
	}
	if has, _ := s.HasStreamKey(a.ID); !has {
		t.Fatal("no key after a new one")
	}
	if err := s.DeleteStreamKey(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountByStreamKey("hash3"); err != ErrNotFound {
		t.Fatalf("deleted key: %v", err)
	}
	// Keys go with their account.
	s.DeleteAccount(b.ID)
	if _, err := s.AccountByStreamKey("hash2"); err != ErrNotFound {
		t.Fatalf("key of a deleted account: %v", err)
	}
}
