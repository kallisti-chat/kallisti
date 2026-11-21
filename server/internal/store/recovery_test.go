package store

import (
	"database/sql"
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

// An imported Mumble user's link: using it makes the account and moves the
// registration (by move, in the same transaction), once.
func TestClaimLink(t *testing.T) {
	s := open(t)
	now := time.UnixMilli(1_700_000_000_000)
	if err := s.SetClaimLink(ClaimLink{User: 7, Name: "Feld", CertHash: "old"}, "t", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimLinkOf("t", now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired link: %v", err)
	}
	link, err := s.ClaimLinkOf("t", now)
	if err != nil || link != (ClaimLink{User: 7, Name: "Feld", CertHash: "old"}) {
		t.Fatalf("link: %+v %v", link, err)
	}

	// A failed move makes no account and keeps the link.
	acc := Account{Name: "Feld", PasswordHash: "pw", CertPEM: []byte("c"), KeyPEM: []byte("k")}
	if _, err := s.Claim("t", acc, now, func(*sql.Tx, ClaimLink) error { return ErrNotFound }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed move: %v", err)
	}
	if _, err := s.AccountByName("Feld"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("account after a failed move: %v", err)
	}

	var moved ClaimLink
	got, err := s.Claim("t", acc, now, func(_ *sql.Tx, l ClaimLink) error { moved = l; return nil })
	if err != nil || got.ID == 0 || got.Name != "Feld" || moved != link {
		t.Fatalf("claim: %+v %v, moved %+v", got, err, moved)
	}
	if a, _ := s.AccountByName("feld"); a.ID != got.ID || a.PasswordHash != "pw" {
		t.Fatalf("account: %+v", a)
	}
	if _, err := s.Claim("t", acc, now, func(*sql.Tx, ClaimLink) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second use: %v", err)
	}
}

// Invite-only servers: an admin's link needs no invitation. A name taken by
// an account meanwhile fails.
func TestClaimLinkNeedsNoInviteButAFreeName(t *testing.T) {
	s := open(t)
	now := time.UnixMilli(1_700_000_000_000)
	if err := s.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	move := func(*sql.Tx, ClaimLink) error { return nil }
	s.SetClaimLink(ClaimLink{User: 7, Name: "feld", CertHash: "old"}, "a", now.Add(time.Hour))
	if _, err := s.Claim("a", Account{Name: "feld", PasswordHash: "pw", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, move); err != nil {
		t.Fatalf("invite-only: %v", err)
	}
	s.SetClaimLink(ClaimLink{User: 8, Name: "feld", CertHash: "other"}, "b", now.Add(time.Hour))
	if _, err := s.Claim("b", Account{Name: "feld", PasswordHash: "pw", CertPEM: []byte("c2"), KeyPEM: []byte("k")}, now, move); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("taken name: %v", err)
	}
}

// Changed roles end claim links too.
func TestDeleteRecoveryLinksEndsClaimLinks(t *testing.T) {
	s := open(t)
	now := time.UnixMilli(1_700_000_000_000)
	s.SetClaimLink(ClaimLink{User: 7, Name: "feld", CertHash: "old"}, "t", now.Add(time.Hour))
	s.DeleteRecoveryLinks()
	if _, err := s.ClaimLinkOf("t", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after roles changed: %v", err)
	}
}
