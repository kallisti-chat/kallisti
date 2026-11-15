package store

import (
	"errors"
	"testing"
	"time"
)

func TestRecoveryLink(t *testing.T) {
	s := open(t)
	now := time.UnixMilli(1_700_000_000_000)
	acc, err := s.CreateAccount(Account{Name: "alice", PasswordHash: "old", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSession("phone", acc.ID, now.Add(time.Hour))

	// A newer link replaces the older one.
	if err := s.SetRecoveryLink(acc.ID, "first", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.SetRecoveryLink(acc.ID, "second", now.Add(time.Hour))
	if _, err := s.RecoveryAccount("first", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replaced link: %v", err)
	}
	if got, err := s.RecoveryAccount("second", now); err != nil || got.ID != acc.ID {
		t.Fatalf("link: %v %v", got, err)
	}
	if _, err := s.RecoveryAccount("second", now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired link: %v", err)
	}

	// Using it sets the password, ends all sessions, and works once.
	if err := s.Recover("second", acc.ID, "new", now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AccountByName("alice"); got.PasswordHash != "new" {
		t.Fatalf("password %q", got.PasswordHash)
	}
	if _, err := s.AccountBySession("phone", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old session: %v", err)
	}
	if err := s.Recover("second", acc.ID, "again", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second use: %v", err)
	}
}

// A login checks the password, then makes its session: not if the password
// changed meanwhile (a recovery or a change ended the sessions already).
func TestSessionOnlyForTheCheckedPassword(t *testing.T) {
	s := open(t)
	now := time.UnixMilli(1_700_000_000_000)
	acc, _ := s.CreateAccount(Account{Name: "alice", PasswordHash: "old", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	s.SetRecoveryLink(acc.ID, "t", now.Add(time.Hour))
	s.Recover("t", acc.ID, "new", now)
	if err := s.CreateSessionChecked("late", acc.ID, "old", now.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session for the old password: %v", err)
	}
	if err := s.CreateSessionChecked("fresh", acc.ID, "new", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}
