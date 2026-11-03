package sidecar

import (
	"fmt"
	"strings"
	"testing"

	"kallisti.chat/server/internal/mumble"
)

func TestRegistrationAdminControls(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	admin := readyAs(t, h, 1, "admin")
	other := readyAs(t, h, 2, "other")
	for _, request := range []string{
		`{"type":"set-invite-only","on":true}`,
		`{"type":"registration-invites"}`,
		`{"type":"create-registration-invite","seconds":86400,"uses":1}`,
		`{"type":"delete-registration-invite","invite":1}`,
	} {
		other.Handle([]byte(request))
		if m := next(t, other); m["type"] != "error" || m["about"] != "manage" {
			t.Fatalf("non-admin: %v", m)
		}
	}
	admin.Handle([]byte(`{"type":"set-invite-only","on":true}`))
	for _, c := range []*Conn{admin, other} {
		if m := next(t, c); m["type"] != "invite-only" || m["on"] != true {
			t.Fatalf("policy: %v", m)
		}
	}
	late := h.Connect()
	defer late.Close()
	late.SetIdentity(mumble.Identity{Session: 4, Name: "late"})
	if m := next(t, late); m["inviteOnly"] != true {
		t.Fatalf("ready: %v", m)
	}
	admin.Handle([]byte(`{"type":"create-registration-invite","seconds":86400,"uses":1}`))
	m := next(t, admin)
	token, _ := m["token"].(string)
	if m["type"] != "registration-invite-created" || len(token) != 32 {
		t.Fatalf("create: %v", m)
	}
	list := next(t, admin)
	if list["type"] != "registration-invites" || strings.Contains(fmt.Sprint(list), token) {
		t.Fatalf("list: %v", list)
	}
	if err := h.store.CheckRegistration(token, false, h.now()); err != nil {
		t.Fatal(err)
	}
	invites, _ := h.store.RegistrationInvites()
	admin.Handle(fmt.Appendf(nil, `{"type":"delete-registration-invite","invite":%d}`, invites[0].ID))
	if m := next(t, admin); len(m["invites"].([]any)) != 0 {
		t.Fatalf("delete: %v", m)
	}
	if err := h.store.CheckRegistration(token, false, h.now()); err == nil {
		t.Fatal("revoked link still valid")
	}
}
