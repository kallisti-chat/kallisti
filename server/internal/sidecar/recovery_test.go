package sidecar

import (
	"errors"
	"testing"
	"time"

	"kallisti.chat/server/internal/store"
)

// Session 1 is an admin; user 7 is carol (a Kallisti account), 8 is a
// Mumble-only registration.
func (f *fakeVoice) MayRecover(actor, userID uint32) (string, error) {
	if actor != 1 {
		return "", errors.New("only admins make recovery links")
	}
	switch userID {
	case 7:
		return "carol", nil
	case 8:
		return "mumbler", nil
	}
	return "", errors.New("no such user")
}

func TestRecoveryLink(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	carol, err := h.store.CreateAccount(store.Account{Name: "carol", PasswordHash: "x", CertPEM: []byte("c"), KeyPEM: []byte("k")}, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	admin := readyAs(t, h, 1, "admin")
	bob := readyAs(t, h, 2, "bob")

	admin.Handle([]byte(`{"type":"recovery-link","user":7}`))
	m := nextOf(t, admin, "recovery-link")
	token, _ := m["token"].(string)
	if m["user"] != float64(7) || len(token) < 20 {
		t.Fatalf("got %v", m)
	}
	if acc, err := h.store.RecoveryAccount(token, h.now()); err != nil || acc.ID != carol.ID {
		t.Fatalf("stored link: %v %v", acc, err)
	}

	// Roles changed (carol may be an admin now, whom this admin may not
	// recover): open links go.
	h.RolesChanged()
	if _, err := h.store.RecoveryAccount(token, h.now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("link after a role change: %v", err)
	}

	bob.Handle([]byte(`{"type":"recovery-link","user":7}`))
	if e := nextOf(t, bob, "error"); e["about"] != "manage" {
		t.Fatalf("bob got %v", e)
	}
	admin.Handle([]byte(`{"type":"recovery-link","user":8}`))
	if e := nextOf(t, admin, "error"); e["about"] != "manage" {
		t.Fatalf("mumble-only user: %v", e)
	}
}
