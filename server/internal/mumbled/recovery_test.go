package mumbled

import (
	"sync/atomic"
	"testing"
)

// Recovery links follow the rule for deleting users: admins only, and for
// an admin's account only SuperUser.
func TestMayRecover(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	// Every name has an account (once all registered), but mallory's
	// certificate is not its own: a Mumble registration that only shares
	// the name.
	var accounts atomic.Bool
	s, addr := startServerAndListen(t, "127.0.0.1", Config{
		DB:               db,
		ReservedName:     func(string) bool { return accounts.Load() },
		WebAccountExists: func(name, _ string) bool { return name != "mallory" },
	}, nil)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	bob, bobID := registered(t, addr, "bob")
	_, carolID := registered(t, addr, "carol")
	s.SetAdmin(su.session, aliceID, true)
	s.SetModerator(su.session, bobID, true)
	_, daveID := registered(t, addr, "dave")
	s.SetAdmin(su.session, daveID, true)
	_, malloryID := registered(t, addr, "mallory")
	accounts.Store(true)

	if name, err := s.MayRecover(alice.session, carolID); err != nil || name != "carol" {
		t.Fatalf("admin for a user: %q %v", name, err)
	}
	if _, err := s.MayRecover(bob.session, carolID); err == nil {
		t.Fatal("a moderator made a recovery link")
	}
	if _, err := s.MayRecover(alice.session, daveID); err == nil {
		t.Fatal("an admin made a link for another admin")
	}
	if name, err := s.MayRecover(su.session, daveID); err != nil || name != "dave" {
		t.Fatalf("SuperUser for an admin: %q %v", name, err)
	}
	if _, err := s.MayRecover(alice.session, malloryID); err == nil {
		t.Fatal("a link for a Mumble registration that only shares an account's name")
	}
	if _, err := s.MayRecover(alice.session, 999); err == nil {
		t.Fatal("a link for nobody")
	}
}
