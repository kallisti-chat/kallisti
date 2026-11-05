package sidecar

import (
	"fmt"
	"testing"

	"kallisti.chat/server/internal/store"
)

// A file link without a signature: the file's chat's readers, as for
// pushes (fakeVoice: the secret mirror 101 for bob (20) only, voice
// channel 9 for nobody); the user is looked up only when a guest may not
// read.
func TestUploadAccessIsTheChatsReaders(t *testing.T) {
	h, _, staff, _, _ := threadHub(t)
	general, secret := h.channels[0].ID, h.channels[1].ID
	thread := startThread(t, h, staff, postTo(t, h, staff, secret, "pictures"), "Food")
	voice9, err := h.store.VoiceChat(9)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := h.store.DirectChat(20, 30)
	if err != nil {
		t.Fatal(err)
	}
	attach := func(channel int64) string {
		t.Helper()
		key := fmt.Sprintf("upload-%d", channel)
		u, err := h.store.AddUpload(store.Upload{Key: key, Author: "mod"}, clock)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.store.AddMessage(store.NewMessage{ChannelID: channel, Thread: channel == thread, Author: "mod", Text: "file", At: clock, Uploads: []store.Attachment{{ID: u.ID}}}); err != nil {
			t.Fatal(err)
		}
		return key
	}
	asked := false
	as := func(user uint32, ok bool) func() (uint32, bool) {
		asked = false
		return func() (uint32, bool) { asked = true; return user, ok }
	}
	for _, c := range []struct {
		channel     int64
		guest, user bool // may read: a guest, carol (30); bob (20) always but voice 9
	}{
		{general, true, true},
		{secret, false, false},
		{thread, false, false},
		{voice9, false, false},
		{direct, true, true}, // files of direct chats stay public
	} {
		key := attach(c.channel)
		if got := h.MayReadUpload(key, as(0, false)); got != c.guest || asked && c.guest {
			t.Fatalf("channel %d, guest: %v (user asked: %v)", c.channel, got, asked)
		}
		if got := h.MayReadUpload(key, as(30, true)); got != c.user {
			t.Fatalf("channel %d, carol: %v", c.channel, got)
		}
		if got := h.MayReadUpload(key, as(20, true)); got != (c.channel != voice9) {
			t.Fatalf("channel %d, bob: %v", c.channel, got)
		}
	}
	// A text channel without a mirror yet is public, as its chat.
	h.mu.Lock()
	h.channels[1].Mumble = 0
	h.mu.Unlock()
	if !h.MayReadUpload(fmt.Sprintf("upload-%d", secret), as(0, false)) {
		t.Fatal("a channel without a mirror is not public")
	}
	u, err := h.store.AddUpload(store.Upload{Key: "unattached", Author: "mod"}, clock)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{u.Key, "missing"} {
		if h.MayReadUpload(key, as(20, true)) {
			t.Fatalf("%s readable", key)
		}
	}
	if _, err := h.store.DeleteThread(thread); err != nil {
		t.Fatal(err)
	}
	if h.MayReadUpload(fmt.Sprintf("upload-%d", thread), as(20, true)) {
		t.Fatal("deleted thread was readable")
	}
}
