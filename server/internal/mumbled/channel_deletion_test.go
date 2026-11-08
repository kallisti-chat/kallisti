package mumbled

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// bufferedMoveServer exercises the real message and voice paths without sockets.
func bufferedMoveServer(state *State, users ...*User) *Server {
	s := &Server{state: state, clients: map[uint32]*client{}}
	for _, u := range users {
		state.AddUser(u)
		state.Refresh(u)
		s.clients[u.Session] = &client{
			session: u.Session, seen: state.Visible(u),
			out: make(chan []byte, 100), voice: make(chan []byte, 10),
			bandwidth: rate.NewLimiter(rate.Inf, maxBandwidth),
		}
	}
	return s
}

func drainMoveMessages(t *testing.T, c *client) []proto.Message {
	t.Helper()
	var messages []proto.Message
	for len(c.out) > 0 {
		frame := <-c.out
		var m proto.Message
		switch binary.BigEndian.Uint16(frame) {
		case mumble.TypeUserState:
			m = &pb.UserState{}
		case mumble.TypeUserRemove:
			m = &pb.UserRemove{}
		case mumble.TypeChannelState:
			m = &pb.ChannelState{}
		case mumble.TypeChannelRemove:
			m = &pb.ChannelRemove{}
		case mumble.TypePermissionQuery:
			m = &pb.PermissionQuery{}
		default:
			t.Fatalf("unexpected frame: %x", frame)
		}
		if err := proto.Unmarshal(frame[6:], m); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, m)
	}
	return messages
}

func TestMoveReconcilesLocationDependentAccess(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		for _, root := range []bool{false, true} {
			t.Run(fmt.Sprintf("deletion=%v/root=%v", deletion, root), func(t *testing.T) {
				state := newTestState(t)
				lobby := state.Channels[0]
				if !root {
					lobby = mustAdd(t, state, 0, "Lobby", false)
				}
				room := mustAdd(t, state, lobby.ID, "BobRoom", false)
				games := mustAdd(t, state, lobby.ID, "Games", false)
				// Listen in Games depends on being below Lobby, not in Lobby.
				// Keep Games visible, so visibility cannot mask leaked audio.
				lobby.ACLs = []ACL{
					{ApplyHere: true, ApplySubs: true, Group: "all", Deny: Listen},
					{ApplyHere: true, ApplySubs: true, Group: "~sub", Allow: Listen},
					{ApplyHere: true, Group: "all", Deny: Speak},
				}
				// A permitted subscription must survive the move too.
				public := mustAdd(t, state, 0, "Public", false)
				public.NoInheritACL = true
				bob := &User{Session: 1, Name: "Bob", Channel: room.ID}
				alice := &User{Session: 2, Name: "Alice", Channel: games.ID}
				s := bufferedMoveServer(state, bob, alice)
				bc, ac := s.clients[bob.Session], s.clients[alice.Session]
				if !state.Has(bob, games.ID, Listen) || bob.Suppress {
					t.Fatal("invalid fixture: Bob must initially have Listen and Speak")
				}
				state.Listen(bob.Session, games.ID, true)
				state.Listen(bob.Session, public.ID, true)
				state.SetTarget(alice.Session, 1, VoiceTarget{Channels: []TargetChannel{{ID: games.ID}}})
				// This primes receiver caches and proves actual tunneled delivery.
				for _, target := range []uint32{0, 1} {
					s.voice(ac, audioTo(target, 1, []byte{0xfc}), true)
					if len(bc.voice) != 1 {
						t.Fatalf("target %d: Bob did not receive audio before move", target)
					}
					frame := <-bc.voice
					p, ok := decodeAudio(frame[6:], false)
					if binary.BigEndian.Uint16(frame) != mumble.TypeUDPTunnel || !ok || p.session != alice.Session || p.context != ContextListen {
						t.Fatalf("unexpected listener audio: %x", frame)
					}
				}
				var moves [][3]uint32
				state.OnMove = func(session, from, to uint32) { moves = append(moves, [3]uint32{session, from, to}) }
				s.mu.Lock()
				if deletion {
					s.removeChannelLocked(room.ID, alice.Session)
				} else {
					s.moveLocked(bob, lobby.ID, alice.Session)
				}
				s.mu.Unlock()
				if !slices.Equal(moves, [][3]uint32{{bob.Session, room.ID, lobby.ID}}) {
					t.Errorf("move callbacks: %v", moves)
				}
				if bob.Channel != lobby.ID || !bob.Suppress || state.Has(bob, games.ID, Listen) {
					t.Errorf("after move: channel=%d suppress=%v Listen=%v", bob.Channel, bob.Suppress, state.Has(bob, games.ID, Listen))
				}
				if bob.Listening[games.ID] || !bob.Listening[public.ID] {
					t.Errorf("subscriptions after move: %v; want only Public", bob.Listening)
				}
				for _, target := range []uint32{0, 1} {
					s.voice(ac, audioTo(target, 2, []byte{0xfc}), true)
					if len(bc.voice) != 0 {
						t.Errorf("target %d: Bob still receives Games audio after losing Listen", target)
						<-bc.voice
					}
				}
				for _, c := range []*client{bc, ac} {
					messages := drainMoveMessages(t, c)
					var moveCount, unlistenCount, removeCount int
					var queries []*pb.PermissionQuery
					for i, message := range messages {
						switch m := message.(type) {
						case *pb.UserState:
							if m.ChannelId != nil {
								moveCount++
								if i != 0 || m.GetSession() != bob.Session || m.GetChannelId() != lobby.ID || m.GetActor() != alice.Session || m.Suppress == nil || !m.GetSuppress() {
									t.Errorf("move must come first with suppression: %v", m)
								}
							}
							if len(m.ListeningChannelRemove) > 0 {
								unlistenCount++
								if m.GetSession() != bob.Session || !slices.Equal(m.ListeningChannelRemove, []uint32{games.ID}) {
									t.Errorf("unlisten: %v", m)
								}
							}
						case *pb.ChannelRemove:
							removeCount++
							if !deletion || i != 1 || m.GetChannelId() != room.ID {
								t.Errorf("channel removal must follow move: %v at %d", m, i)
							}
						case *pb.PermissionQuery:
							queries = append(queries, m)
						}
					}
					if moveCount != 1 || unlistenCount != 1 || (deletion && removeCount != 1) {
						t.Errorf("session %d: moves=%d unlistens=%d removals=%d", c.session, moveCount, unlistenCount, removeCount)
					}
					if c == bc {
						wantChannels := []uint32{lobby.ID}
						if !root {
							wantChannels = append(wantChannels, 0)
						}
						if len(queries) != len(wantChannels) {
							t.Errorf("permission queries: %v; want channels %v", queries, wantChannels)
						} else {
							for i, id := range wantChannels {
								if queries[i].GetChannelId() != id || queries[i].GetFlush() != (i == 0) || queries[i].GetPermissions() != uint32(state.Permissions(bob, id)) {
									t.Errorf("stale permission query: %v", queries[i])
								}
							}
						}
					} else if len(queries) != 0 {
						t.Errorf("unmoved client got permission flush: %v", queries)
					}
				}
			})
		}
	}
}

func TestChannelDeletionReconcilesViewsAfterAllMoveAndRemovalMessages(t *testing.T) {
	state := newTestState(t)
	room := mustAdd(t, state, 0, "BobRoom", false)
	below := mustAdd(t, state, room.ID, "Below", false)
	hide(room, 7, 8)
	room.ACLs = append(room.ACLs, ACL{ApplyHere: true, ApplySubs: true, Group: "all", Deny: Speak})
	hidden := mustAdd(t, state, 0, "Hidden", false)
	hiddenChild := mustAdd(t, state, hidden.ID, "HiddenChild", false)
	// @sub,-1 tests the user's location relative to this channel's parent.
	hidden.ACLs = []ACL{
		{ApplyHere: true, ApplySubs: true, Group: "all", Deny: Traverse},
		{ApplyHere: true, ApplySubs: true, Group: "~sub,-1", Allow: Traverse},
	}
	revealed := mustAdd(t, state, 0, "Revealed", false)
	revealed.ACLs = []ACL{
		{ApplyHere: true, Group: "all", Deny: Traverse},
		{ApplyHere: true, Group: "!sub,-1", Allow: Traverse},
		{ApplyHere: true, HasUser: true, UserID: 10, Allow: Traverse},
	}
	bob := &User{Session: 1, Name: "Bob", Registered: true, UserID: 7, Channel: room.ID}
	carol := &User{Session: 2, Name: "Carol", Registered: true, UserID: 8, Channel: below.ID}
	alice := &User{Session: 3, Name: "Alice", Channel: hiddenChild.ID}
	dave := &User{Session: 4, Name: "Dave", Registered: true, UserID: 10, Channel: revealed.ID}
	observer := &User{Session: 5, Name: "Observer"}
	s := bufferedMoveServer(state, bob, carol, alice, dave, observer)
	for _, u := range []*User{bob, carol} {
		c := s.clients[u.Session]
		if !u.Suppress || !c.seen[hiddenChild.ID] || c.seen[revealed.ID] {
			t.Fatal("invalid initial suppression or visibility")
		}
	}
	moves := map[uint32][][2]uint32{}
	state.OnMove = func(session, from, to uint32) { moves[session] = append(moves[session], [2]uint32{from, to}) }
	s.mu.Lock()
	removed := s.removeChannelLocked(room.ID, 0)
	s.mu.Unlock()
	if !slices.Equal(removed, []uint32{below.ID, room.ID}) {
		t.Fatalf("removed order: %v", removed)
	}
	if len(moves) != 2 || !slices.Equal(moves[bob.Session], [][2]uint32{{room.ID, 0}}) || !slices.Equal(moves[carol.Session], [][2]uint32{{below.ID, 0}}) {
		t.Errorf("move callbacks: %v", moves)
	}
	for _, u := range []*User{bob, carol} {
		c := s.clients[u.Session]
		if u.Channel != 0 || u.Suppress || c.seen[room.ID] || c.seen[below.ID] || c.seen[hidden.ID] || c.seen[hiddenChild.ID] || !c.seen[revealed.ID] {
			t.Errorf("session %d: channel=%d suppress=%v seen=%v", u.Session, u.Channel, u.Suppress, c.seen)
		}
		var events []string
		for _, message := range drainMoveMessages(t, c) {
			switch m := message.(type) {
			case *pb.UserState:
				if m.GetSession() == bob.Session || m.GetSession() == carol.Session {
					if m.ChannelId == nil || m.GetChannelId() != 0 || m.Suppress == nil || m.GetSuppress() || m.Actor != nil {
						t.Errorf("server move must clear suppression without actor: %v", m)
					}
				} else if m.GetSession() == dave.Session && (m.Name == nil || m.GetChannelId() != revealed.ID) {
					t.Errorf("newly visible user needs full state: %v", m)
				}
				events = append(events, fmt.Sprintf("user %d", m.GetSession()))
			case *pb.UserRemove:
				events = append(events, fmt.Sprintf("hide user %d", m.GetSession()))
			case *pb.ChannelRemove:
				events = append(events, fmt.Sprintf("remove %d", m.GetChannelId()))
			case *pb.ChannelState:
				events = append(events, fmt.Sprintf("show %d", m.GetChannelId()))
			case *pb.PermissionQuery:
				events = append(events, "permissions")
			}
		}
		want := []string{
			"user 1", "user 2", // all moves precede channel removals
			fmt.Sprintf("remove %d", below.ID), fmt.Sprintf("remove %d", room.ID),
			"permissions", "hide user 3", // users go before hidden channels
			fmt.Sprintf("remove %d", hiddenChild.ID), fmt.Sprintf("remove %d", hidden.ID),
			fmt.Sprintf("show %d", revealed.ID), "user 4", // channels before new users
		}
		if !slices.Equal(events, want) {
			t.Errorf("session %d events:\n got %v\nwant %v", u.Session, events, want)
		}
	}
	// An observer that never saw the deleted tree learns the full moved users,
	// but must not get removals for unknown channels.
	messages := drainMoveMessages(t, s.clients[observer.Session])
	if len(messages) != 2 {
		t.Fatalf("observer messages: %v", messages)
	}
	for i, message := range messages {
		m, ok := message.(*pb.UserState)
		if !ok || m.GetSession() != uint32(i+1) || m.Name == nil || m.GetChannelId() != 0 || m.GetSuppress() {
			t.Errorf("observer needs full moved user state: %v", message)
		}
	}
}
