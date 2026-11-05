package mumbled

import "testing"

// Offline access (push, file links without a signature) follows the
// current ACL: private parents, staff, revocation, bans, and the
// certificate's own ACL entries; a guest only public channels.
func TestOfflineAccessUsesCurrentPermissions(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	club := su.createChannel(0, "food pictures", false)
	child := su.createChannel(club, "recipes", false)
	mod, _, modID := webUser(t, s, "mod")
	if err := s.SetModerator(su.session, modID, true); err != nil {
		t.Fatal(err)
	}
	_, _, aliceID := webUser(t, s, "alice")
	_, _, bobID := webUser(t, s, "bob")
	check := func(user *uint32, channel uint32, want bool) {
		t.Helper()
		sees, mayRead := s.GuestSees(channel), s.GuestMayRead(channel)
		if user != nil {
			sees, mayRead = s.UserSees(*user, channel), s.UserMayRead(*user, channel)
		}
		if sees != want || mayRead != want {
			t.Fatalf("user=%v channel=%d: sees %v, may read %v, want %v", user, channel, sees, mayRead, want)
		}
	}
	check(nil, club, true)
	if err := s.SetPrivate(mod.session, club, true, []uint32{aliceID}); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []uint32{club, child} {
		check(nil, channel, false)
		check(&bobID, channel, false)
		check(&aliceID, channel, true)
		check(&modID, channel, true)
	}
	if err := s.SetPrivate(mod.session, club, true, []uint32{bobID}); err != nil {
		t.Fatal(err)
	}
	check(&aliceID, club, false)
	check(&bobID, child, true)
	// A deny for bob's certificate counts, as for a connected bob.
	changeACL(s, club, func(c *Channel) {
		c.ACLs = append(c.ACLs, ACL{ApplyHere: true, ApplySubs: true, Group: "$bob-hash", Deny: Enter | Traverse})
	})
	check(&bobID, club, false)
	check(&bobID, child, false)
	changeACL(s, club, func(c *Channel) { c.ACLs = c.ACLs[:len(c.ACLs)-1] })
	s.bans.Store(&[]BanEntry{{Hash: "bob-hash"}})
	check(&bobID, club, false)
	check(&modID, 99999, false)
	if err := s.SetPrivate(mod.session, club, false, nil); err != nil {
		t.Fatal(err)
	}
	check(nil, child, true)
}

func TestUserByCertHash(t *testing.T) {
	s, _ := rolesServer(t)
	_, _, aliceID := webUser(t, s, "alice")
	if id, ok := s.UserByCertHash("alice-hash"); !ok || id != aliceID {
		t.Fatalf("got %d %v", id, ok)
	}
	for _, hash := range []string{"", "unknown-hash"} {
		if _, ok := s.UserByCertHash(hash); ok {
			t.Fatalf("%q found", hash)
		}
	}
}
