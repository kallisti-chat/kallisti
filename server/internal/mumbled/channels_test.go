package mumbled

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// createChannel makes a channel as the official client does and returns
// its id from the server's answer.
func (c *testClient) createChannel(parent uint32, name string, temporary bool) uint32 {
	c.t.Helper()
	c.send(mumble.TypeChannelState, &pb.ChannelState{Parent: proto.Uint32(parent), Name: proto.String(name), Temporary: proto.Bool(temporary)})
	for {
		var m pb.ChannelState
		proto.Unmarshal(c.expect(mumble.TypeChannelState), &m)
		if m.GetName() == name {
			return m.GetChannelId()
		}
	}
}

func (c *testClient) move(session, channel uint32) {
	c.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(session), ChannelId: proto.Uint32(channel)})
}

// waitForMove reads UserStates until the user is in the channel.
func (c *testClient) waitForMove(session, channel uint32) {
	c.t.Helper()
	for {
		var m pb.UserState
		proto.Unmarshal(c.expect(mumble.TypeUserState), &m)
		if m.GetSession() == session && m.ChannelId != nil && m.GetChannelId() == channel {
			return
		}
	}
}

func TestChannelsSeparateVoice(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	games := alice.createChannel(0, "Games", false)
	alice.move(alice.session, games)
	bob.waitForMove(alice.session, games)
	alice.conn.Write(mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(1, []byte{0xfc})))
	bob.expectNone(mumble.TypeUDPTunnel)
	bob.move(bob.session, games)
	alice.waitForMove(bob.session, games)
	alice.conn.Write(mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(2, []byte{0xfc})))
	bob.expect(mumble.TypeUDPTunnel)
}

func TestNewUsersSeeTheChannelTree(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	a := alice.createChannel(0, "A", false)
	b := alice.createChannel(a, "B", false)
	alice.send(mumble.TypeChannelState, &pb.ChannelState{ChannelId: proto.Uint32(b), LinksAdd: []uint32{0}})
	alice.move(alice.session, b)
	alice.waitForMove(alice.session, b)
	c, _ := tryDial(t, addr, "bob")
	parents := map[uint32]uint32{}
	linked := false
	for {
		f := <-c.frames
		if f.typ == mumble.TypeChannelState {
			var m pb.ChannelState
			proto.Unmarshal(f.body, &m)
			if m.Parent != nil {
				parents[m.GetChannelId()] = m.GetParent()
			}
			linked = linked || (m.GetChannelId() == b && len(m.GetLinks()) == 1)
		}
		if f.typ == mumble.TypeUserState {
			var m pb.UserState
			proto.Unmarshal(f.body, &m)
			if m.GetName() == "alice" && m.GetChannelId() != b {
				t.Fatalf("alice in channel %d", m.GetChannelId())
			}
		}
		if f.typ == mumble.TypeServerSync {
			break
		}
	}
	if parents[a] != 0 || parents[b] != a || !linked {
		t.Fatalf("parents %v linked %v", parents, linked)
	}
}

func TestRemovingAChannelMovesUsersAndTellsEveryone(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	a := alice.createChannel(0, "A", false)
	bob.move(bob.session, a)
	alice.waitForMove(bob.session, a)
	alice.send(mumble.TypeChannelRemove, &pb.ChannelRemove{ChannelId: proto.Uint32(a)})
	bob.waitForMove(bob.session, 0)
	var m pb.ChannelRemove
	proto.Unmarshal(bob.expect(mumble.TypeChannelRemove), &m)
	if m.GetChannelId() != a {
		t.Fatalf("removed %v", &m)
	}
}

func TestTemporaryChannelsMoveTheCreatorAndVanish(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	tmp := alice.createChannel(0, "Tmp", true)
	bob.waitForMove(alice.session, tmp)
	alice.move(alice.session, 0)
	var m pb.ChannelRemove
	proto.Unmarshal(bob.expect(mumble.TypeChannelRemove), &m)
	if m.GetChannelId() != tmp {
		t.Fatalf("removed %v", &m)
	}
}

func TestBadChannelNamesAreDenied(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	alice.send(mumble.TypeChannelState, &pb.ChannelState{Parent: proto.Uint32(0), Name: proto.String("<script>")})
	var d pb.PermissionDenied
	proto.Unmarshal(alice.expect(mumble.TypePermissionDenied), &d)
	if d.GetType() != pb.PermissionDenied_ChannelName {
		t.Fatalf("denied %v", &d)
	}
}

func TestTextGoesToChannelsTreesAndUsers(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	carol := dial(t, addr, "carol")
	a := alice.createChannel(0, "A", false)
	b := alice.createChannel(a, "B", false)
	bob.move(bob.session, b)
	carol.waitForMove(bob.session, b)
	// To the root channel: carol (in root) gets it, bob (in B) does not.
	alice.send(mumble.TypeTextMessage, &pb.TextMessage{ChannelId: []uint32{0}, Message: proto.String("root")})
	carol.expect(mumble.TypeTextMessage)
	bob.expectNone(mumble.TypeTextMessage)
	// To the tree of A: bob gets it.
	alice.send(mumble.TypeTextMessage, &pb.TextMessage{TreeId: []uint32{a}, Message: proto.String("tree")})
	bob.expect(mumble.TypeTextMessage)
	carol.expectNone(mumble.TypeTextMessage)
	// To bob only.
	alice.send(mumble.TypeTextMessage, &pb.TextMessage{Session: []uint32{bob.session}, Message: proto.String("psst")})
	var m pb.TextMessage
	proto.Unmarshal(bob.expect(mumble.TypeTextMessage), &m)
	if m.GetMessage() != "psst" || len(m.GetSession()) != 1 {
		t.Fatalf("bob got %v", &m)
	}
	carol.expectNone(mumble.TypeTextMessage)
}

func TestChannelsSurviveARestart(t *testing.T) {
	db := testDB(t)
	addr := startServer(t, "127.0.0.1", Config{DB: db}, openACL)
	alice := dial(t, addr, "alice")
	games := alice.createChannel(0, "Games", false)
	alice.createChannel(0, "Tmp", true) // not saved
	addr2 := startServer(t, "127.0.0.1", Config{DB: db}, openACL)
	c, _ := tryDial(t, addr2, "bob")
	found := map[string]uint32{}
	for {
		f := <-c.frames
		if f.typ == mumble.TypeChannelState {
			var m pb.ChannelState
			proto.Unmarshal(f.body, &m)
			found[m.GetName()] = m.GetChannelId()
		}
		if f.typ == mumble.TypeServerSync {
			break
		}
	}
	if found["Games"] != games || found["Tmp"] != 0 {
		t.Fatalf("channels after restart: %v", found)
	}
}

func TestTreeTextSkipsChannelsWithoutPermission(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	a := su.createChannel(0, "A", false)
	closed := su.createChannel(a, "Closed", false)
	su.setACL(closed, true, []*pb.ACL_ChanACL{chanACL("all", true, true, 0, TextMessage)}, nil)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	bob.move(bob.session, a)
	alice.waitForMove(bob.session, a)
	alice.send(mumble.TypeTextMessage, &pb.TextMessage{TreeId: []uint32{0}, Message: proto.String("all")})
	bob.expect(mumble.TypeTextMessage)
	alice.expectNone(mumble.TypePermissionDenied)
}

func TestABigServerAcceptsNewClients(t *testing.T) {
	n := 1000
	_, addr := startServerAndListen(t, "127.0.0.1", Config{}, func(s *Server) {
		for i := range n {
			s.state.AddChannel(0, fmt.Sprintf("Channel %d", i), false)
		}
	})
	c, err := tryDial(t, addr, "alice")
	if err != nil {
		t.Fatal(err)
	}
	channels := 0
	timeout := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				t.Fatalf("closed after %d channels", channels)
			}
			if f.typ == mumble.TypeChannelState {
				channels++
			}
			if f.typ == mumble.TypeServerSync {
				if channels != n+1 {
					t.Fatalf("%d channels before ServerSync", channels)
				}
				return
			}
		case <-timeout:
			t.Fatal("no ServerSync")
		}
	}
}

// A change that could not be saved is told to who made it: it holds only
// until the next restart.
func TestAFailedSaveIsToldToTheActor(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	if _, err := s.cfg.DB.db.Exec(`ALTER TABLE mumble_channels RENAME TO gone`); err != nil {
		t.Fatal(err)
	}
	su.createChannel(0, "Music", false)
	var m pb.TextMessage
	proto.Unmarshal(su.expect(mumble.TypeTextMessage), &m)
	if !strings.Contains(m.GetMessage(), "not saved") {
		t.Fatalf("got %q", m.GetMessage())
	}
	s.cfg.DB.db.Exec(`ALTER TABLE gone RENAME TO mumble_channels`)
}
