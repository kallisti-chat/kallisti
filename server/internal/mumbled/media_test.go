package mumbled

import "testing"

func TestRoomMediaUsesCurrentPermissions(t *testing.T) {
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
	check := func(hash string, channel uint32, want bool) {
		t.Helper()
		for _, text := range []bool{true, false} {
			if got := s.MayReadMedia(hash, channel, text); got != want {
				t.Fatalf("hash=%q channel=%d text=%v: got %v want %v", hash, channel, text, got, want)
			}
		}
	}
	check("", club, true)
	if err := s.SetPrivate(mod.session, club, true, []uint32{aliceID}); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []uint32{club, child} {
		check("", channel, false)
		check("bob-hash", channel, false)
		check("unknown-hash", channel, false)
		check("alice-hash", channel, true)
		check("mod-hash", channel, true)
	}
	// Revoke membership: no cached grant survives the change.
	if err := s.SetPrivate(mod.session, club, true, []uint32{bobID}); err != nil {
		t.Fatal(err)
	}
	check("alice-hash", club, false)
	check("bob-hash", club, true)
	check("bob-hash", child, true)
	check("mod-hash", child, true)
	s.bans.Store(&[]BanEntry{{Hash: "bob-hash"}})
	check("bob-hash", club, false)
	check("bob-hash", child, false)
	check("mod-hash", 99999, false)
	if err := s.SetPrivate(mod.session, club, false, nil); err != nil {
		t.Fatal(err)
	}
	check("", club, true)
	check("", child, true)
}

func TestRoomMediaPrivateTextMirrors(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	_, _, aliceID := webUser(t, s, "alice")
	_, _, _ = webUser(t, s, "bob")
	ids, err := s.SyncTextChannels([]TextMirror{{Name: "general"}, {Name: "food-pictures", Private: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrivate(su.session, ids[1], true, []uint32{aliceID}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		hash    string
		channel uint32
		want    bool
	}{
		{"", ids[0], true}, {"", ids[1], false}, {"bob-hash", ids[1], false}, {"alice-hash", ids[1], true},
	} {
		if got := s.MayReadMedia(c.hash, c.channel, true); got != c.want {
			t.Fatalf("%+v got %v", c, got)
		}
	}
}
