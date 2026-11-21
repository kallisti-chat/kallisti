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
	// Every name but feld's has an account (once all registered), but
	// mallory's certificate is not its own: a Mumble registration that only
	// shares the name. feld is an imported Mumble user without an account.
	var accounts atomic.Bool
	s, addr := startServerAndListen(t, "127.0.0.1", Config{
		DB:               db,
		ReservedName:     func(key string) bool { return accounts.Load() && key != "feld" },
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
	_, feldID := registered(t, addr, "feld")
	accounts.Store(true)

	if name, claim, err := s.MayRecover(alice.session, carolID); err != nil || name != "carol" || claim != "" {
		t.Fatalf("admin for a user: %q %v", name, err)
	}
	if _, _, err := s.MayRecover(bob.session, carolID); err == nil {
		t.Fatal("a moderator made a recovery link")
	}
	if _, _, err := s.MayRecover(alice.session, daveID); err == nil {
		t.Fatal("an admin made a link for another admin")
	}
	if name, _, err := s.MayRecover(su.session, daveID); err != nil || name != "dave" {
		t.Fatalf("SuperUser for an admin: %q %v", name, err)
	}
	if _, _, err := s.MayRecover(alice.session, malloryID); err == nil {
		t.Fatal("a link for a Mumble registration that only shares an account's name")
	}
	// A registration without an account: its link makes one, for its
	// certificate.
	r, _ := db.UserByID(feldID)
	if name, claim, err := s.MayRecover(alice.session, feldID); err != nil || name != "feld" || claim == "" || claim != r.CertHash {
		t.Fatalf("imported user: %q %q %v", name, claim, err)
	}
	if _, _, err := s.MayRecover(bob.session, feldID); err == nil {
		t.Fatal("a moderator made a link for an imported user")
	}
	if _, _, err := s.MayRecover(alice.session, 999); err == nil {
		t.Fatal("a link for nobody")
	}
}
