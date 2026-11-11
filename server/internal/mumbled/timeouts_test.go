package mumbled

import (
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// timeoutServer: SuperUser's password is "admin"; changed counts the
// OnTimeoutsChanged calls.
func timeoutServer(t *testing.T) (*Server, string, *DB, *atomic.Int32) {
	t.Helper()
	db := testDB(t)
	setAdminPassword(db, "admin")
	changed := &atomic.Int32{}
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db, OnTimeoutsChanged: func() { changed.Add(1) }}, nil)
	return s, addr, db, changed
}

func muted(on bool) func(*pb.UserState) bool {
	return func(m *pb.UserState) bool { return m.Mute != nil && m.GetMute() == on }
}

func TestTimeOutRules(t *testing.T) {
	s, addr, _, _ := timeoutServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	carol, carolID := registered(t, addr, "carol")
	s.SetModerator(su.session, carolID, true)
	bob := dial(t, addr, "bob")
	guest := dial(t, addr, "guest")
	for _, c := range []struct {
		name          string
		actor, target uint32
		d             time.Duration
	}{
		{"a plain user", bob.session, guest.session, time.Hour},
		{"a moderator on a moderator", alice.session, carol.session, time.Hour},
		{"on SuperUser", alice.session, su.session, time.Hour},
		{"on oneself", alice.session, alice.session, time.Hour},
		{"too long", alice.session, guest.session, maxTimeout + time.Hour},
		{"negative", alice.session, guest.session, -time.Hour},
	} {
		if err := s.SetTimeout(c.actor, c.target, c.d); err == nil {
			t.Errorf("%s: no error", c.name)
		}
	}
	// Admins time out moderators.
	if err := s.SetTimeout(su.session, carol.session, time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestTimedOutUsersAreMutedAndCannotWrite(t *testing.T) {
	s, addr, _, changed := timeoutServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	bob, bobID := registered(t, addr, "bob")
	if err := s.SetTimeout(alice.session, bob.session, time.Hour); err != nil {
		t.Fatal(err)
	}
	bob.waitForState(bob.session, muted(true))
	if until, ok := s.TimedOut(bob.session); !ok || time.Until(until) < 59*time.Minute {
		t.Fatalf("timed out %v %v", until, ok)
	}
	if got := s.Timeouts(); len(got) != 1 || !got[0].Registered || got[0].UserID != bobID {
		t.Fatalf("timeouts %+v", got)
	}
	if changed.Load() != 1 {
		t.Fatalf("changed %d times", changed.Load())
	}
	// No texts; nobody unmutes the user while it lasts.
	bob.send(mumble.TypeTextMessage, &pb.TextMessage{ChannelId: []uint32{0}, Message: proto.String("hi")})
	bob.expectDenied(pb.PermissionDenied_Text)
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(bob.session), Mute: proto.Bool(false)})
	alice.expectDenied(pb.PermissionDenied_Text)
	// Removed early: unmuted.
	if err := s.SetTimeout(alice.session, bob.session, 0); err != nil {
		t.Fatal(err)
	}
	bob.waitForState(bob.session, muted(false))
	if _, ok := s.TimedOut(bob.session); ok || len(s.Timeouts()) != 0 {
		t.Fatal("still timed out")
	}
}

func TestTimeOutsEnd(t *testing.T) {
	s, addr, _, changed := timeoutServer(t)
	su := superUser(t, addr)
	guest := dial(t, addr, "guest")
	if err := s.SetTimeout(su.session, guest.session, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	guest.waitForState(guest.session, muted(true))
	guest.waitForState(guest.session, muted(false))
	// The hook runs after the unmute went out (and the time-out was saved).
	eventually(t, func() bool { return changed.Load() == 2 })
	if len(s.Timeouts()) != 0 {
		t.Fatalf("timeouts %+v", s.Timeouts())
	}
}

func TestTimeOutsLastOverReconnectsAndRestarts(t *testing.T) {
	s, addr, db, _ := timeoutServer(t)
	su := superUser(t, addr)
	cert := clientCert(t)
	bob := dialAs(t, addr, dialOpts{name: "bob", cert: cert})
	bob.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(bob.session), UserId: proto.Uint32(0)})
	bobID := waitForUserID(t, bob, bob.session)
	if err := s.SetTimeout(su.session, bob.session, time.Hour); err != nil {
		t.Fatal(err)
	}
	bob.conn.Close()
	again := dialAs(t, addr, dialOpts{name: "bob", cert: cert})
	if _, ok := s.TimedOut(again.session); !ok {
		t.Fatal("not timed out after a reconnect")
	}
	s.mu.Lock()
	mute := s.state.Users[again.session].Mute
	s.mu.Unlock()
	if !mute {
		t.Fatal("not muted after a reconnect")
	}
	restarted, err := New(Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if got := restarted.Timeouts(); len(got) != 1 || got[0].UserID != bobID {
		t.Fatalf("after a restart: %+v", got)
	}
}

func TestGuestTimeOutsEndWhenTheyLeave(t *testing.T) {
	s, addr, _, changed := timeoutServer(t)
	su := superUser(t, addr)
	guest := dial(t, addr, "guest")
	s.SetTimeout(su.session, guest.session, time.Hour)
	guest.conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for len(s.Timeouts()) != 0 || changed.Load() != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timeouts %+v, changed %d", s.Timeouts(), changed.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isMuted(s *Server, session uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Users[session].Mute
}

func TestEndingATimeOutKeepsAnEarlierMute(t *testing.T) {
	s, addr, _, _ := timeoutServer(t)
	su := superUser(t, addr)
	guest := dial(t, addr, "guest")
	if err := s.SetTimeout(su.session, guest.session, 0); err == nil {
		t.Fatal("ended a time-out that was not there")
	}
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(guest.session), Mute: proto.Bool(true)})
	guest.waitForState(guest.session, muted(true))
	s.SetTimeout(su.session, guest.session, time.Hour)
	s.SetTimeout(su.session, guest.session, 2*time.Hour) // a new time keeps it too
	if err := s.SetTimeout(su.session, guest.session, 0); err != nil {
		t.Fatal(err)
	}
	if !isMuted(s, guest.session) {
		t.Fatal("unmuted")
	}
}

func TestTimedOutGuestsCannotRegister(t *testing.T) {
	s, addr, _, _ := timeoutServer(t)
	su := superUser(t, addr)
	guest := dialAs(t, addr, dialOpts{name: "guest", cert: clientCert(t)})
	s.SetTimeout(su.session, guest.session, time.Hour)
	guest.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(guest.session), UserId: proto.Uint32(0)})
	guest.expectDenied(pb.PermissionDenied_Text)
}

func TestUnregisteringEndsATimeOut(t *testing.T) {
	s, addr, _, _ := timeoutServer(t)
	su := superUser(t, addr)
	bob, bobID := registered(t, addr, "bob")
	s.SetTimeout(su.session, bob.session, time.Hour)
	su.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(bobID)}}})
	bob.waitForState(bob.session, muted(false))
	if len(s.Timeouts()) != 0 {
		t.Fatalf("timeouts %+v", s.Timeouts())
	}
}
