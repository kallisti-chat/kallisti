package mumbled

import (
	"encoding/binary"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

func TestSelfUnmuteRefreshesReceiverRouting(t *testing.T) {
	for _, route := range []struct {
		name     string
		target   uint32
		context  AudioContext
		listener bool
	}{
		{"channel", 0, ContextNormal, false},
		{"whisper", 1, ContextWhisper, false},
		{"shout", 2, ContextShout, false},
		{"listener", 0, ContextListen, true},
		{"shout-listener", 2, ContextListen, true},
	} {
		for _, change := range []struct {
			name               string
			mute, deaf         *bool
			wantMute, wantDeaf bool
		}{
			{"implicit-undeafen", proto.Bool(false), nil, false, false},
			{"explicit-undeafen", nil, proto.Bool(false), true, false},
			{"explicit-unmute-undeafen", proto.Bool(false), proto.Bool(false), false, false},
			{"explicit-mute-undeafen", proto.Bool(true), proto.Bool(false), true, false},
			{"explicit-unmute-deafen", proto.Bool(false), proto.Bool(true), true, true},
			{"explicit-mute-deafen", proto.Bool(true), proto.Bool(true), true, true},
			{"remain-muted", proto.Bool(true), nil, true, true},
			{"remain-deafened", nil, proto.Bool(true), true, true},
		} {
			t.Run(route.name+"/"+change.name, func(t *testing.T) {
				state := newTestState(t)
				room := mustAdd(t, state, 0, "Room", false)
				alice := &User{Session: 1, Name: "Alice", Channel: room.ID}
				bob := &User{Session: 2, Name: "Bob", Channel: room.ID}
				if route.listener {
					bob.Channel = 0
				}
				s := bufferedMoveServer(state, alice, bob)
				ac, bc := s.clients[alice.Session], s.clients[bob.Session]
				state.SetTarget(alice.Session, 1, VoiceTarget{Sessions: []uint32{bob.Session}})
				state.SetTarget(alice.Session, 2, VoiceTarget{Channels: []TargetChannel{{ID: room.ID}}})
				if route.listener {
					state.Listen(bob.Session, room.ID, true)
				}
				checkAudio := func(seq uint64, hear bool) {
					t.Helper()
					s.voice(ac, audioTo(route.target, seq, []byte{0xfc}), true)
					want := 0
					if hear {
						want = 1
					}
					if len(bc.voice) != want {
						t.Fatalf("frame %d: got %d audio frames, want %d", seq, len(bc.voice), want)
					}
					if hear {
						frame := <-bc.voice
						p, ok := decodeAudio(frame[6:], false)
						if binary.BigEndian.Uint16(frame) != mumble.TypeUDPTunnel || !ok || p.session != alice.Session || p.context != route.context || p.frame != seq || !slices.Equal(p.opus, []byte{0xfc}) {
							t.Fatalf("unexpected audio: %x", frame)
						}
					}
					cache, ok := alice.cache[route.target]
					if !ok || cache.gen != state.gen {
						t.Fatal("voice did not prime the receiver cache")
					}
				}
				checkAudio(1, true)
				s.userStateMsg(bc, &pb.UserState{SelfDeaf: proto.Bool(true)})
				if !bob.SelfMute || !bob.SelfDeaf {
					t.Fatal("self-deafening must also mute")
				}
				for _, c := range []*client{ac, bc} {
					drainMoveMessages(t, c)
				}
				checkAudio(2, false) // Prime the deafened receiver set before unmuting.
				before := state.gen
				s.userStateMsg(bc, &pb.UserState{SelfMute: change.mute, SelfDeaf: change.deaf})
				if bob.SelfMute != change.wantMute || bob.SelfDeaf != change.wantDeaf {
					t.Fatalf("got mute=%v deaf=%v, want mute=%v deaf=%v", bob.SelfMute, bob.SelfDeaf, change.wantMute, change.wantDeaf)
				}
				checkAudio(3, !change.wantDeaf)
				if (state.gen != before) != !change.wantDeaf {
					t.Error("routing must be invalidated exactly when self-deaf changes")
				}
				for _, c := range []*client{ac, bc} {
					messages := drainMoveMessages(t, c)
					if len(messages) != 1 {
						t.Fatalf("session %d: want one self-state broadcast, got %v", c.session, messages)
					}
					m, ok := messages[0].(*pb.UserState)
					if !ok || m.GetSession() != bob.Session || m.SelfMute == nil || m.SelfDeaf == nil || m.GetSelfMute() != change.wantMute || m.GetSelfDeaf() != change.wantDeaf {
						t.Errorf("incorrect self-state broadcast: %v", messages[0])
					}
				}
			})
		}
	}
}
