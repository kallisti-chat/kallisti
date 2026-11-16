package mumbled

import (
	"testing"

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
	if _, _, err := s.APISession("no such hash"); err == nil {
		t.Fatal("an API session for nobody")
	}
}
