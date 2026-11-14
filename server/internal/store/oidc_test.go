package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOIDCIdentityTransactions(t *testing.T) {
	s := open(t)
	now := time.Now()
	id := OIDCIdentity{Issuer: "https://id.example", Subject: "stable-subject"}
	acc, err := s.CreateOIDCAccount(registrationAccount("alice"), id, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.AccountByOIDC(id); err != nil || got.ID != acc.ID {
		t.Fatalf("identity: %+v %v", got, err)
	}
	if got, err := s.OIDCIdentityOf(acc.ID); err != nil || got != id {
		t.Fatalf("reverse identity: %+v %v", got, err)
	}
	if _, err := s.AccountByOIDC(OIDCIdentity{Issuer: "https://other.example", Subject: id.Subject}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("issuer conflated: %v", err)
	}
	if _, err := s.CreateOIDCAccount(registrationAccount("other"), id, now, ""); !errors.Is(err, ErrOIDCTaken) {
		t.Fatalf("duplicate identity: %v", err)
	}
	if _, err := s.AccountByName("other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partially created: %v", err)
	}
	if err := s.CreateOIDCSession("session", acc.ID, id, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkOIDC(acc.ID, "session", "", now); !errors.Is(err, ErrLastLogin) {
		t.Fatalf("last login removed: %v", err)
	}
	if err := s.ChangePassword(acc.ID, "session", "", "hash", now); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOIDCSession("other-session", acc.ID, id, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkOIDC(acc.ID, "session", "stale-hash", now); err == nil {
		t.Fatal("unlink accepted stale password")
	}
	if err := s.UnlinkOIDC(acc.ID, "missing-session", "hash", now); err == nil {
		t.Fatal("unlink accepted invalid session")
	}
	if _, err := s.AccountBySession("other-session", now); err != nil {
		t.Fatal("failed unlink revoked another session")
	}
	if err := s.UnlinkOIDC(acc.ID, "session", "hash", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountBySession("other-session", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unlink retained another session: %v", err)
	}
	if err := s.CreateOIDCSession("stale-login", acc.ID, id, now.Add(time.Hour)); err == nil {
		t.Fatal("removed identity minted session")
	}
	if _, err := s.AccountBySession("stale-login", now); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale session persisted")
	}
	if _, err := s.AccountBySession("session", now); err != nil {
		t.Fatal("unlink ended own password session")
	}
}

func TestOIDCLinkSnapshotsAndAccountDeletion(t *testing.T) {
	s := open(t)
	now := time.Now()
	guest := registrationAccount("guest")
	guest.Temporary = true
	acc, err := s.CreateAccount(guest, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("guest-session", acc.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	id := OIDCIdentity{Issuer: "https://id.example", Subject: "subject"}
	for _, tc := range []struct {
		session, hash string
		at            time.Time
	}{{"missing", "", now}, {"guest-session", "wrong", now}, {"guest-session", "", now.Add(2 * time.Hour)}} {
		if err := s.LinkOIDC(acc.ID, tc.session, tc.hash, id, tc.at); !errors.Is(err, ErrSessionGone) {
			t.Fatalf("invalid snapshot: %v", err)
		}
	}
	if err := s.LinkOIDC(acc.ID, "guest-session", "", id, now); err != nil {
		t.Fatal(err)
	}
	if got, err := s.AccountByName("guest"); err != nil || got.Temporary {
		t.Fatalf("guest upgrade: %+v %v", got, err)
	}
	if err := s.LinkOIDC(acc.ID, "guest-session", "", OIDCIdentity{Issuer: id.Issuer, Subject: "second"}, now); !errors.Is(err, ErrOIDCTaken) {
		t.Fatalf("second identity: %v", err)
	}
	if err := s.DeleteAccount(acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OIDCIdentityOf(acc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("identity survived deletion: %v", err)
	}
	if _, err := s.CreateOIDCAccount(registrationAccount("replacement"), id, now, ""); err != nil {
		t.Fatalf("deleted identity not reusable: %v", err)
	}
}

func TestOIDCInviteRollbackAndConcurrentRegistration(t *testing.T) {
	s := open(t)
	now := time.Now()
	id := OIDCIdentity{Issuer: "https://id.example", Subject: "subject"}
	if _, err := s.CreateOIDCAccount(registrationAccount("alice"), id, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRegistrationInvite("once", RegistrationInvite{Creator: "admin", Created: now.UnixMilli(), MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOIDCAccount(registrationAccount("failed"), id, now, "once"); !errors.Is(err, ErrOIDCTaken) {
		t.Fatalf("duplicate signup: %v", err)
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		wg.Go(func() {
			_, err := s.CreateOIDCAccount(registrationAccount(name), OIDCIdentity{Issuer: id.Issuer, Subject: name}, now, "once")
			outcomes <- err
		})
	}
	wg.Wait()
	close(outcomes)
	success, denied := 0, 0
	for err := range outcomes {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRegistrationInvite) {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || denied != 1 {
		t.Fatalf("success=%d denied=%d", success, denied)
	}
	if _, err := s.AccountByName("failed"); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed transaction created an account")
	}
}

func TestOIDCSchemaUpgradePreservesAccountsAndSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	existing := registrationAccount("existing")
	existing.PasswordHash = "preserved-hash"
	acc, err := s.CreateAccount(existing, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("existing-session", acc.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE oidc_identities"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.AccountBySession("existing-session", now)
	if err != nil || got.ID != acc.ID || got.PasswordHash != "preserved-hash" || string(got.CertPEM) != "cert" || string(got.KeyPEM) != "key" {
		t.Fatalf("upgrade lost account/session: %+v %v", got, err)
	}
	if id, err := s.OIDCIdentityOf(acc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("spurious identity: %+v %v", id, err)
	}
	if err := s.LinkOIDC(acc.ID, "existing-session", "preserved-hash", OIDCIdentity{Issuer: "https://id.example", Subject: "subject"}, now); err != nil {
		t.Fatal(err)
	}
}
