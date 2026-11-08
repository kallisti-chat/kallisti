package mumbled

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"kallisti.chat/server/internal/mumble/pb"
)

func removalOrderChannels() []Channel {
	// IDs deliberately do not encode depth, and input is not sibling-sorted.
	return []Channel{
		{ID: 70, Parent: 80, Name: "SecondLeaf"},
		{ID: 50, Name: "Removed"},
		{ID: 90, Parent: 10, Name: "HighLeaf"},
		{ID: 80, Parent: 50, Name: "SecondBranch"},
		{ID: 5, Parent: 80, Name: "LowLeaf"},
		{ID: 20, Parent: 10, Name: "FirstLeaf"},
		{ID: 10, Parent: 50, Name: "FirstBranch"},
		{ID: 100, Name: "Outside"},
	}
}

func TestSubtreeDeterministicPostorder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		channels []Channel
		id       uint32
		want     []uint32
	}{
		{"leaf", nil, 0, []uint32{0}},
		{"three-level-chain", []Channel{{ID: 10}, {ID: 20, Parent: 10}, {ID: 30, Parent: 20}}, 10, []uint32{30, 20, 10}},
		{"sibling-branches", removalOrderChannels(), 50, []uint32{20, 90, 10, 5, 70, 80, 50}},
		{"whole-tree", removalOrderChannels(), 0, []uint32{20, 90, 10, 5, 70, 80, 50, 100, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewState("Root", tc.channels)
			for range 20 {
				got := state.Subtree(tc.id)
				if !slices.Equal(got, tc.want) {
					t.Fatalf("Subtree(%d) = %v, want %v", tc.id, got, tc.want)
				}
				seen := map[uint32]bool{}
				for _, id := range got {
					if seen[id] || id != tc.id && seen[state.Channels[id].Parent] {
						t.Fatalf("duplicate or parent before child: %v", got)
					}
					seen[id] = true
				}
			}
		})
	}
}

func TestChannelRemovalBroadcastsPostorderAfterMoves(t *testing.T) {
	state := NewState("Root", removalOrderChannels())
	state.Channels[0].ACLs = []ACL{{ApplyHere: true, ApplySubs: true, Group: "all", Deny: Speak}}
	state.Channels[50].ACLs = []ACL{{ApplyHere: true, ApplySubs: true, Group: "all", Allow: Speak}}
	wantRemoved := []uint32{20, 90, 10, 5, 70, 80, 50}
	var users []*User
	from := map[uint32]uint32{}
	for i, id := range wantRemoved {
		session := uint32(i + 1)
		users = append(users, &User{Session: session, Name: fmt.Sprintf("User%d", session), Channel: id})
		from[session] = id
	}
	observer := &User{Session: 99, Name: "Observer", Channel: 100}
	users = append(users, observer)
	s := bufferedMoveServer(state, users...)
	state.Link(90, 100)
	state.Listen(observer.Session, 90, true)
	state.Listen(observer.Session, 100, true)
	for _, u := range users[:len(users)-1] {
		if u.Suppress {
			t.Fatal("invalid fixture: moved users must initially have Speak")
		}
	}
	moves := map[uint32][][2]uint32{}
	state.OnMove = func(session, from, to uint32) { moves[session] = append(moves[session], [2]uint32{from, to}) }
	s.mu.Lock()
	removed := s.removeChannelLocked(50, observer.Session)
	s.mu.Unlock()
	if !slices.Equal(removed, wantRemoved) {
		t.Errorf("removed %v, want %v", removed, wantRemoved)
	}
	if len(moves) != len(from) {
		t.Errorf("move callbacks: %v", moves)
	}
	for session, old := range from {
		u := state.Users[session]
		if !slices.Equal(moves[session], [][2]uint32{{old, 0}}) || u.Channel != 0 || !u.Suppress {
			t.Errorf("session %d: callbacks=%v channel=%d suppress=%v", session, moves[session], u.Channel, u.Suppress)
		}
	}
	if !maps.Equal(observer.Listening, map[uint32]bool{100: true}) || len(state.Channels[100].Links) != 0 || len(state.Channels) != 2 {
		t.Errorf("incomplete deletion cleanup: listening=%v channels=%v", observer.Listening, state.Channels)
	}
	for _, c := range s.clients {
		var gotMoves, gotRemoved []uint32
		for _, message := range drainMoveMessages(t, c) {
			switch m := message.(type) {
			case *pb.UserState:
				if m.ChannelId != nil {
					if len(gotRemoved) != 0 || m.GetChannelId() != 0 || m.Suppress == nil || !m.GetSuppress() || m.GetActor() != observer.Session {
						t.Errorf("session %d: move must precede removals and set suppression: %v", c.session, m)
					}
					gotMoves = append(gotMoves, m.GetSession())
				}
			case *pb.ChannelRemove:
				gotRemoved = append(gotRemoved, m.GetChannelId())
			}
		}
		if !slices.Equal(gotMoves, []uint32{1, 2, 3, 4, 5, 6, 7}) || !slices.Equal(gotRemoved, wantRemoved) {
			t.Errorf("session %d: moves=%v removals=%v", c.session, gotMoves, gotRemoved)
		}
		for _, id := range wantRemoved {
			if c.seen[id] {
				t.Errorf("session %d still sees removed channel %d", c.session, id)
			}
		}
		if !c.seen[0] || !c.seen[100] {
			t.Errorf("session %d lost surviving channels: %v", c.session, c.seen)
		}
	}
}
