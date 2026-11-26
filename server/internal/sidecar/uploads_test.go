package sidecar

import (
	"errors"
	"testing"

	"kallisti.chat/server/internal/mumble"
)

func (f *fakeVoice) RandomImageNames() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.randomImageNames
}

func (f *fakeVoice) SetRandomImageNames(session uint32, enabled bool) (bool, error) {
	if session != 1 {
		return false, errors.New("only admins change this setting")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.randomImageNamesError != nil {
		return false, f.randomImageNamesError
	}
	f.randomImageNames = enabled
	return enabled, nil
}

func TestRandomImageNames(t *testing.T) {
	h := newHub(t, "general")
	v := &fakeVoice{}
	h.SetVoice(v, "")
	admin := h.Connect()
	defer admin.Close()
	admin.SetIdentity(mumble.Identity{Session: 1, Name: "admin"})
	if m := next(t, admin); m["type"] != "ready" || m["randomImageNames"] != false {
		t.Fatalf("unexpected ready: %v", m)
	}
	bob := readyAs(t, h, 2, "bob")
	bob.Handle([]byte(`{"type":"set-random-image-names","randomImageNames":true}`))
	if m := next(t, bob); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("unauthorized update: %v", m)
	}
	empty(t, admin)
	for _, request := range []string{
		`{"type":"set-random-image-names"}`,
		`{"type":"set-random-image-names","randomImageNames":null}`,
	} {
		admin.Handle([]byte(request))
		if m := next(t, admin); m["type"] != "error" || m["about"] != "manage" {
			t.Fatalf("missing value: %v", m)
		}
		empty(t, bob)
	}
	for _, enabled := range []bool{true, false} {
		value := "false"
		if enabled {
			value = "true"
		}
		admin.Handle([]byte(`{"type":"set-random-image-names","randomImageNames":` + value + `}`))
		for _, c := range []*Conn{admin, bob} {
			if m := next(t, c); m["type"] != "random-image-names" || m["randomImageNames"] != enabled {
				t.Fatalf("unexpected update: %v", m)
			}
			empty(t, c)
		}
	}
	v.mu.Lock()
	v.randomImageNamesError = errors.New("save failed")
	v.mu.Unlock()
	admin.Handle([]byte(`{"type":"set-random-image-names","randomImageNames":true}`))
	if m := next(t, admin); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("save failure: %v", m)
	}
	empty(t, bob)
}

func TestRandomImageNamesExternalServer(t *testing.T) {
	h := newHub(t, "general")
	admin := h.Connect()
	defer admin.Close()
	admin.SetIdentity(mumble.Identity{Session: 1, Name: "admin"})
	if m := next(t, admin); m["type"] != "ready" || m["voiceChats"] != false {
		t.Fatalf("external server ready: %v", m)
	}
	admin.Handle([]byte(`{"type":"set-random-image-names","randomImageNames":true}`))
	if m := next(t, admin); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("external server update: %v", m)
	}
}
