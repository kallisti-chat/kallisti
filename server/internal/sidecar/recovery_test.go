package sidecar

import (
	"errors"
	"testing"
	"time"

	"kallisti.chat/server/internal/store"
)

// Session 1 is an admin; user 7 is carol (a Kallisti account), 8 a Mumble
// registration that shares an account's name, 9 feld (imported, no
// account: claimed by the certificate "feldcert").
func (f *fakeVoice) MayRecover(actor, userID uint32) (string, string, error) {
	if f.duringRecover != nil {
		f.duringRecover()
	}
	if actor != 1 {
		return "", "", errors.New("only admins make recovery links")
	}
	switch userID {
	case 7:
		return "carol", "", nil
	case 8:
		return "", "", errors.New("a Kallisti account has this user's name")
	case 9:
		return "feld", "feldcert", nil
	}
	return "", "", errors.New("no such user")
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

	// An imported user: a claim link for the registration, no account yet.
	admin.Handle([]byte(`{"type":"recovery-link","user":9}`))
	m = nextOf(t, admin, "recovery-link")
	token, _ = m["token"].(string)
	if l, err := h.store.ClaimLinkOf(token, h.now()); err != nil || l != (store.ClaimLink{User: 9, Name: "feld", CertHash: "feldcert"}) {
		t.Fatalf("claim link: %+v %v", l, err)
	}
	if _, err := h.store.AccountByName("feld"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("account before the link is used: %v", err)
	}
}

// Roles that change while a link is made (the user became an admin after
// the check) end that link too.
func TestRolesChangedWhileMakingALink(t *testing.T) {
	h := newHub(t, "general")
	v := &fakeVoice{}
	h.SetVoice(v, "")
	h.store.CreateAccount(store.Account{Name: "carol", PasswordHash: "x", CertPEM: []byte("c"), KeyPEM: []byte("k")}, time.Now(), "")
	admin := readyAs(t, h, 1, "admin")
	changed := make(chan struct{})
	v.duringRecover = func() {
		go func() { h.RolesChanged(); close(changed) }()
		time.Sleep(50 * time.Millisecond)
	}
	admin.Handle([]byte(`{"type":"recovery-link","user":7}`))
	token, _ := nextOf(t, admin, "recovery-link")["token"].(string)
	<-changed
	if _, err := h.store.RecoveryAccount(token, h.now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("link made across a role change: %v", err)
	}
}
