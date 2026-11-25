package mumbled

import (
	"slices"
	"testing"
)

func TestStaffMakeATextChannelReadOnly(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	bob, _ := registered(t, addr, "bob")
	carol, carolID := registered(t, addr, "carol")
	ids, _ := s.SyncTextChannels([]TextMirror{{Name: "announcements"}})
	news := ids[0]
	s.mu.Lock()
	before := slices.Clone(s.state.Channels[news].ACLs)
	s.mu.Unlock()

	if err := s.SetReadOnlyChannel(alice.session, news, true, []uint32{carolID}); err != nil {
		t.Fatal(err)
	}
	if got := s.ReadOnlyChannels(); len(got) != 1 || got[0].Channel != news || !slices.Equal(got[0].Speakers, []Speaker{{carolID, "carol"}}) {
		t.Fatalf("read-only %+v", got)
	}
	if s.MayWrite(bob.session, news) || !s.MayWrite(carol.session, news) || !s.MayWrite(alice.session, news) || !s.MayWrite(su.session, news) {
		t.Fatal("wrong writers")
	}

	// Off again: only the read-only entries go.
	if err := s.SetReadOnlyChannel(alice.session, news, false, nil); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	after := s.state.Channels[news].ACLs
	s.mu.Unlock()
	if !slices.Equal(after, before) || len(s.ReadOnlyChannels()) != 0 || !s.MayWrite(bob.session, news) {
		t.Fatalf("ACL %+v, want %+v", after, before)
	}
}

func TestReadOnlyNeedsStaffAndATextChannel(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	bob, _ := registered(t, addr, "bob")
	ids, _ := s.SyncTextChannels([]TextMirror{{Name: "announcements"}})
	if err := s.SetReadOnlyChannel(bob.session, ids[0], true, nil); err == nil {
		t.Fatal("a user made it read-only")
	}
	music := su.createChannel(0, "Music", false)
	if err := s.SetReadOnlyChannel(su.session, music, true, nil); err == nil {
		t.Fatal("a voice channel made read-only")
	}
}
