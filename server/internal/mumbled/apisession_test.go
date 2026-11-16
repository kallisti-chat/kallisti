package mumbled

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"

	"kallisti.chat/server/internal/msgrules"
)

// An API session (agent API, no connection) gets the registered user's
// rights: chat checks, moderation, message rules.
func TestAPISessionHasTheUsersRights(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	_, aliceID := registered(t, addr, "alice")
	r, err := s.cfg.DB.UserByID(aliceID)
	if err != nil {
		t.Fatal(err)
	}
	sess, id, err := s.APISession(r.CertHash)
	if err != nil || id != aliceID {
		t.Fatal(id, err)
	}
	if !s.Sees(sess, 0) || !s.MayWrite(sess, 0) || !s.MayRead(sess, 0) {
		t.Fatal("no chat rights in root")
	}
	if s.MayManage(sess) {
		t.Fatal("alice manages")
	}
	if _, err := s.CheckMessage(sess, msgrules.Message{Text: "hello", Post: true}); err != nil {
		t.Fatalf("message rules: %v", err)
	}
	s.SetModerator(su.session, aliceID, true)
	if !s.MayManage(sess) {
		t.Fatal("a moderator's API session cannot manage")
	}
	// A time-out holds for the API session too.
	if _, out := s.TimedOut(sess); out {
		t.Fatal("timed out before")
	}
	if err := s.SetTimeout(su.session, aliceSession(t, s, aliceID), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, out := s.TimedOut(sess); !out {
		t.Fatal("a time-out does not hold for the API session")
	}
	if _, _, err := s.APISession("no such hash"); err == nil {
		t.Fatal("an API session for nobody")
	}
}

// aliceSession: the online session of a registered user.
func aliceSession(t *testing.T, s *Server, id uint32) uint32 {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.state.Users {
		if u.Registered && u.UserID == id {
			return u.Session
		}
	}
	t.Fatal("not online")
	return 0
}

// Mumble clients get an API post with the author's name, not an unknown
// session as its actor.
func TestAPISessionTextHasTheName(t *testing.T) {
	s, addr := rolesServer(t)
	_, aliceID := registered(t, addr, "alice")
	r, _ := s.cfg.DB.UserByID(aliceID)
	sess, _, err := s.APISession(r.CertHash)
	if err != nil {
		t.Fatal(err)
	}
	bob := dial(t, addr, "bob")
	s.SendChannelText(sess, 0, "hello")
	var m pb.TextMessage
	proto.Unmarshal(bob.expect(mumble.TypeTextMessage), &m)
	if m.Actor != nil || m.GetMessage() != "alice: hello" {
		t.Fatalf("got actor %v %q", m.Actor, m.GetMessage())
	}
}
