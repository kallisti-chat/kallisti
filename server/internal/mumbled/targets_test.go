package mumbled

import (
	"maps"
	"slices"
	"testing"
)

// targetState: root > A > A1, and B; users 1 (root), 2 (A), 3 (A1), 4 (B).
func targetState(t *testing.T) (*State, *Channel, *Channel, *Channel) {
	t.Helper()
	s := NewState("Root", nil)
	s.Channels[0].ACLs, s.Channels[0].Groups = DefaultRootACL()
	a := mustAdd(t, s, 0, "A", false)
	a1 := mustAdd(t, s, a.ID, "A1", false)
	b := mustAdd(t, s, 0, "B", false)
	for i, ch := range []uint32{0, a.ID, a1.ID, b.ID} {
		s.AddUser(&User{Session: uint32(i + 1), Name: string(rune('a' + i)), Channel: ch})
	}
	return s, a, a1, b
}

func receivers(s *State, sender, target uint32) map[uint32]AudioContext {
	return maps.Clone(s.Receivers(sender, target))
}

func TestNormalSpeechAndLoopback(t *testing.T) {
	s, a, _, _ := targetState(t)
	s.MoveUser(4, a.ID)
	if got := receivers(s, 2, 0); !maps.Equal(got, map[uint32]AudioContext{4: ContextNormal}) {
		t.Fatalf("normal: %v", got)
	}
	if got := receivers(s, 2, loopbackTarget); !maps.Equal(got, map[uint32]AudioContext{2: ContextNormal}) {
		t.Fatalf("loopback: %v", got)
	}
}

func TestWhisperToUsers(t *testing.T) {
	s, _, _, _ := targetState(t)
	s.SetTarget(1, 5, VoiceTarget{Sessions: []uint32{3, 4, 1}})
	if got := receivers(s, 1, 5); !maps.Equal(got, map[uint32]AudioContext{3: ContextWhisper, 4: ContextWhisper}) {
		t.Fatalf("whisper: %v", got) // not to the speaker itself
	}
	if len(s.Receivers(1, 6)) != 0 {
		t.Fatal("an unset target reached someone")
	}
}

func TestShoutToChannelsWithChildrenAndGroups(t *testing.T) {
	s, a, _, b := targetState(t)
	s.SetTarget(1, 2, VoiceTarget{Channels: []TargetChannel{{ID: a.ID, Children: true}}})
	if got := receivers(s, 1, 2); !maps.Equal(got, map[uint32]AudioContext{2: ContextShout, 3: ContextShout}) {
		t.Fatalf("shout with children: %v", got)
	}
	// Users who are in a shouted channel do not get a second, whisper copy.
	s.SetTarget(1, 3, VoiceTarget{Channels: []TargetChannel{{ID: b.ID}}, Sessions: []uint32{4, 2}})
	if got := receivers(s, 1, 3); !maps.Equal(got, map[uint32]AudioContext{4: ContextShout, 2: ContextWhisper}) {
		t.Fatalf("shout plus whisper: %v", got)
	}
	// Group filter: only registered users.
	s.Users[3].Registered, s.Users[3].UserID = true, 9
	s.SetTarget(1, 4, VoiceTarget{Channels: []TargetChannel{{ID: a.ID, Children: true, Group: "auth"}}})
	if got := receivers(s, 1, 4); !maps.Equal(got, map[uint32]AudioContext{3: ContextShout}) {
		t.Fatalf("group: %v", got)
	}
}

func TestWhisperNeedsPermission(t *testing.T) {
	s, _, _, b := targetState(t)
	b.ACLs = []ACL{{ApplyHere: true, ApplySubs: true, Group: "all", Deny: Whisper}}
	s.Changed()
	s.SetTarget(1, 1, VoiceTarget{Sessions: []uint32{4, 2}, Channels: []TargetChannel{{ID: b.ID}}})
	if got := receivers(s, 1, 1); !maps.Equal(got, map[uint32]AudioContext{2: ContextWhisper}) {
		t.Fatalf("receivers %v", got)
	}
}

// A shout into a channel needs Speak there too: not into a listen-only
// channel. A whisper to a user in it stays (it is to that user alone).
func TestShoutNeedsSpeakInTheChannel(t *testing.T) {
	s, _, _, b := targetState(t)
	b.ACLs = []ACL{listenOnlyDeny}
	s.Changed()
	s.SetTarget(1, 1, VoiceTarget{Channels: []TargetChannel{{ID: b.ID}}})
	if got := receivers(s, 1, 1); len(got) != 0 {
		t.Fatalf("shout: receivers %v", got)
	}
	s.SetTarget(1, 1, VoiceTarget{Sessions: []uint32{4}})
	if got := receivers(s, 1, 1); !maps.Equal(got, map[uint32]AudioContext{4: ContextWhisper}) {
		t.Fatalf("whisper: receivers %v", got)
	}
}

func TestListenersHearAChannel(t *testing.T) {
	s, a, _, _ := targetState(t)
	s.Listen(4, a.ID, true)
	if got := receivers(s, 2, 0); !maps.Equal(got, map[uint32]AudioContext{4: ContextListen}) {
		t.Fatalf("listen: %v", got)
	}
	s.Users[4].SelfDeaf = true
	s.Changed()
	if len(s.Receivers(2, 0)) != 0 {
		t.Fatal("a deaf listener hears")
	}
	s.Users[4].SelfDeaf = false
	s.Listen(4, a.ID, false)
	if len(s.Receivers(2, 0)) != 0 {
		t.Fatal("still listening")
	}
}

func TestMutedSpeakersReachNobody(t *testing.T) {
	s, _, _, _ := targetState(t)
	s.SetTarget(1, 1, VoiceTarget{Sessions: []uint32{2}})
	s.Users[1].SelfMute = true
	if len(s.Receivers(1, 1)) != 0 || len(s.Receivers(1, loopbackTarget)) != 0 {
		t.Fatal("a muted user was heard")
	}
}

func TestRemovedUsersLeaveTargetsAndListeners(t *testing.T) {
	s, a, _, _ := targetState(t)
	s.SetTarget(1, 1, VoiceTarget{Sessions: []uint32{2}})
	s.Receivers(1, 1)
	s.RemoveUser(2)
	if len(s.Receivers(1, 1)) != 0 {
		t.Fatal("whisper to a user who left")
	}
	s.Listen(4, a.ID, true)
	s.RemoveChannel(a.ID)
	if len(s.Users[4].Listening) != 0 {
		t.Fatal("listening to a removed channel")
	}
}

func TestListenersWithoutListenAreDropped(t *testing.T) {
	s, a, _, b := targetState(t)
	s.Listen(1, a.ID, true)
	s.Listen(1, b.ID, true)
	s.Listen(4, a.ID, true)
	a.ACLs = append(a.ACLs, ACL{ApplyHere: true, Group: "all", Deny: Listen})
	got := s.DropListeners()
	if !maps.EqualFunc(got, map[uint32][]uint32{1: {a.ID}, 4: {a.ID}}, slices.Equal) {
		t.Fatalf("dropped %v", got)
	}
	if !maps.Equal(s.Users[1].Listening, map[uint32]bool{b.ID: true}) || len(s.Users[4].Listening) != 0 {
		t.Fatalf("listening %v %v", s.Users[1].Listening, s.Users[4].Listening)
	}
}

func TestGroupFiltersForOneChannelAddUp(t *testing.T) {
	s, a, _, _ := targetState(t)
	s.Users[2].Registered, s.Users[2].UserID = true, 7 // in A
	a.Groups = map[string]*Group{"mods": newGroup("mods")}
	s.SetTarget(1, 1, VoiceTarget{Channels: []TargetChannel{
		{ID: a.ID, Group: "auth"},
		{ID: a.ID, Group: "mods"},
	}})
	if got := receivers(s, 1, 1); !maps.Equal(got, map[uint32]AudioContext{2: ContextShout}) {
		t.Fatalf("receivers %v: auth or mods", got)
	}
}

func TestATargetChangeKeepsOtherCaches(t *testing.T) {
	s, a, _, _ := targetState(t)
	s.MoveUser(4, a.ID)
	s.Receivers(2, 0)
	gen := s.gen
	s.SetTarget(1, 1, VoiceTarget{Sessions: []uint32{2}})
	if s.gen != gen {
		t.Fatal("a voice target cleared every cache")
	}
}

// A target can name a channel that is gone since (a temporary channel
// empties): no crash on the next voice packet, and no one there.
func TestTargetsWithARemovedChannel(t *testing.T) {
	s, a, _, _ := targetState(t)
	s.SetTarget(1, 1, VoiceTarget{Channels: []TargetChannel{{ID: a.ID, Links: true, Children: true}}})
	s.RemoveChannel(a.ID)
	if got := receivers(s, 1, 1); len(got) != 0 {
		t.Fatalf("receivers %v", got)
	}
}
