package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func registrationAccount(name string) Account {
	return Account{Name: name, CertPEM: []byte("cert"), KeyPEM: []byte("key")}
}

func TestRegistrationPolicyPersistsAndDefaultsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if on, err := s.InviteOnly(); err != nil || on {
		t.Fatalf("default: %v %v", on, err)
	}
	if _, err := s.CreateAccount(registrationAccount("existing"), time.Now()); err != nil {
		t.Fatal(err)
	}
	// Simulate the schema before this feature: upgrading adds only the
	// policy/invitation tables and must not close registration or lose users.
	if _, err := s.db.Exec(`DROP TABLE registration_invites; DROP TABLE registration_policy`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if on, err := s.InviteOnly(); err != nil || on {
		t.Fatalf("upgrade default: %v %v", on, err)
	}
	if err := s.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if on, err := s.InviteOnly(); err != nil || !on {
		t.Fatalf("reopen: %v %v", on, err)
	}
	if _, err := s.AccountByName("existing"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(registrationAccount("blocked"), time.Now()); !errors.Is(err, ErrRegistrationInvite) {
		t.Fatalf("closed: %v", err)
	}
	if err := s.SetInviteOnly(false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(registrationAccount("reopened"), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationInviteAtomicUseAndRollback(t *testing.T) {
	s := open(t)
	now := time.Now()
	if _, err := s.CreateAccount(registrationAccount("taken"), now); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRegistrationInvite("single", RegistrationInvite{Creator: "admin", Created: now.UnixMilli(), MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccountWithInvite(registrationAccount("taken"), now, "single"); !errors.Is(err, ErrNameTaken) {
		t.Fatal(err)
	}
	// A failed insert must not spend the invitation. Concurrent requests
	// for its final use produce exactly one account.
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := range 12 {
		wg.Go(func() {
			_, err := s.CreateAccountWithInvite(registrationAccount(fmt.Sprintf("user%d", i)), now, "single")
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrRegistrationInvite) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d", accepted.Load())
	}
	list, err := s.RegistrationInvites()
	if err != nil || len(list) != 1 || list[0].Uses != 1 {
		t.Fatalf("uses: %v %v", list, err)
	}
}

func TestRegistrationInviteExpiryRevocationAndSeparation(t *testing.T) {
	s := open(t)
	now := time.Now()
	if err := s.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddInvite("channel-hash", Invite{Channel: 1, Creator: "admin", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"expired", "revoked", "valid"} {
		expires := now.Add(time.Hour).UnixMilli()
		if token == "expired" {
			expires = now.UnixMilli()
		}
		if err := s.AddRegistrationInvite(token, RegistrationInvite{Creator: "admin", Created: now.UnixMilli(), Expires: expires, MaxUses: 2}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := s.RegistrationInvites()
	if err := s.DeleteRegistrationInvite(list[1].ID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "unknown", "channel-hash", "expired", "revoked"} {
		if err := s.CheckRegistration(token, false, now); !errors.Is(err, ErrRegistrationInvite) {
			t.Fatalf("check %q: %v", token, err)
		}
		if _, err := s.CreateAccountWithInvite(registrationAccount("new"), now, token); !errors.Is(err, ErrRegistrationInvite) {
			t.Fatalf("create %q: %v", token, err)
		}
	}
	guest := registrationAccount("guest")
	guest.Temporary = true
	if _, err := s.CreateAccountWithInvite(guest, now, "valid"); !errors.Is(err, ErrRegistrationInvite) {
		t.Fatalf("guest: %v", err)
	}
	if _, err := s.CreateAccountWithInvite(registrationAccount("accepted"), now, "valid"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckRegistration("valid", false, now.Add(time.Hour)); !errors.Is(err, ErrRegistrationInvite) {
		t.Fatal(err)
	}
}

func TestRegistrationFederationRollbackAndLatePolicyCheck(t *testing.T) {
	s := open(t)
	now := time.Now()
	id := FediIdentity{Host: "example.org", RemoteID: "1", Handle: "@alice@example.org"}
	if _, err := s.CreateFediAccount(registrationAccount("alice"), id, now); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckRegistration("", false, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFediAccount(registrationAccount("late"), FediIdentity{Host: id.Host, RemoteID: "2"}, now); !errors.Is(err, ErrRegistrationInvite) {
		t.Fatalf("late signup: %v", err)
	}
	if err := s.AddRegistrationInvite("one", RegistrationInvite{Creator: "admin", Created: now.UnixMilli(), MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFediAccountWithInvite(registrationAccount("duplicate"), id, now, "one"); !errors.Is(err, ErrFediTaken) {
		t.Fatal(err)
	}
	if _, err := s.AccountByName("duplicate"); !errors.Is(err, ErrNotFound) {
		t.Fatal("partial account remained")
	}
	id.RemoteID = "2"
	if _, err := s.CreateFediAccountWithInvite(registrationAccount("bob"), id, now, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccountWithInvite(registrationAccount("other"), now, "one"); !errors.Is(err, ErrRegistrationInvite) {
		t.Fatal(err)
	}
}

func TestRegistrationInviteStateSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s != nil {
			s.Close()
		}
	})
	now := time.Now()
	if err := s.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"used", "revoked", "unused"} {
		if err := s.AddRegistrationInvite(token, RegistrationInvite{Creator: "admin", Created: now.UnixMilli(), Expires: now.Add(time.Hour).UnixMilli(), MaxUses: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateAccountWithInvite(registrationAccount("existing"), now, "used"); err != nil {
		t.Fatal(err)
	}
	list, err := s.RegistrationInvites()
	if err != nil || len(list) != 3 {
		t.Fatalf("invitations: %v %v", list, err)
	}
	if err := s.DeleteRegistrationInvite(list[1].ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "used", "revoked"} {
		if _, err := s.CreateAccountWithInvite(registrationAccount("blocked"), now, token); !errors.Is(err, ErrRegistrationInvite) {
			t.Fatalf("after restart %q: %v", token, err)
		}
	}
	if _, err := s.AccountByName("existing"); err != nil {
		t.Fatalf("invited account lost: %v", err)
	}
	if _, err := s.CreateAccountWithInvite(registrationAccount("new"), now, "unused"); err != nil {
		t.Fatalf("unused invitation: %v", err)
	}
	if err := s.CheckRegistration("unused", false, now); !errors.Is(err, ErrRegistrationInvite) {
		t.Fatalf("single-use limit after restart: %v", err)
	}
}

// A stale revoke (another admin's old list) must not hit a newer link.
func TestRegistrationInviteIdsAreNotReused(t *testing.T) {
	s := open(t)
	now := time.Now()
	add := func(token string) int64 {
		if err := s.AddRegistrationInvite(token, RegistrationInvite{Creator: "admin", Created: now.UnixMilli()}); err != nil {
			t.Fatal(err)
		}
		list, err := s.RegistrationInvites()
		if err != nil || len(list) == 0 {
			t.Fatal(list, err)
		}
		return list[0].ID
	}
	old := add("old")
	if err := s.DeleteRegistrationInvite(old); err != nil {
		t.Fatal(err)
	}
	if id := add("new"); id == old {
		t.Fatalf("id %d used again", id)
	}
}
