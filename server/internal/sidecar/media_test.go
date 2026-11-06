package sidecar

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/filelink"
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

// With private-room media on, a chat's readers get signed links (see
// filelink): in messages, previews, history and Mumble clients' text.
func TestFileLinksAreSigned(t *testing.T) {
	h := newHub(t, "general")
	signer := &filelink.Signer{Key: []byte("k"), Now: func() time.Time { return clock }}
	h.FileLinks = signer
	h.publicBase = "https://chat.example"
	alice := ready(t, h, "alice")
	if _, err := h.store.AddUpload(store.Upload{Key: "abc", Name: "cat.png", Type: "image/png", Size: 3, Author: "alice"}, clock); err != nil {
		t.Fatal(err)
	}
	signed := func(link, want string) {
		t.Helper()
		u, err := url.Parse(link)
		if err != nil || u.Path != "/files/abc/cat.png" || !signer.Valid("abc", u.Query()) || u.Query().Has("preview") != (want == "preview") {
			t.Fatalf("link %q", link)
		}
	}
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"","upload":"abc"}`))
	up := next(t, alice)["message"].(map[string]any)["upload"].(map[string]any)
	signed(up["url"].(string), "")
	h.store.SetUploadPreview("abc", "image/webp")
	h.UploadPreview("abc")
	up = next(t, alice)["message"].(map[string]any)["upload"].(map[string]any)
	signed(up["preview"].(string), "preview")
	alice.Handle([]byte(`{"type":"history","channel":1}`))
	up = next(t, alice)["messages"].([]any)[0].(map[string]any)["upload"].(map[string]any)
	signed(up["url"].(string), "")
	m, err := h.store.MessageByUpload("abc")
	if err != nil {
		t.Fatal(err)
	}
	link, ok := strings.CutPrefix(h.mumbleText(m), "https://chat.example")
	if !ok {
		t.Fatalf("mumble text %q", h.mumbleText(m))
	}
	signed(link, "")
}

// A client asks for fresh links before they expire: the messages come
// again, to it only, as edits; not the ones it may not read or without
// files.
func TestFreshFileLinks(t *testing.T) {
	h, _, staff, bob, carol := threadHub(t)
	now := clock
	h.FileLinks = &filelink.Signer{Key: []byte("k"), Now: func() time.Time { return now }}
	secret := h.channels[1].ID
	u, err := h.store.AddUpload(store.Upload{Key: "abc", Name: "cat.png", Author: "mod"}, clock)
	if err != nil {
		t.Fatal(err)
	}
	m, err := h.store.AddMessage(store.NewMessage{ChannelID: secret, Author: "mod", Text: "file", At: clock, Uploads: []store.Attachment{{ID: u.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	plain := postTo(t, h, staff, secret, "no file")
	now = now.Add(filelink.Life)
	do(bob, `{"type":"file-links","messages":[%d,%d,%d]}`, m.ID, plain, m.ID)
	got := next(t, bob)
	link := got["message"].(map[string]any)["upload"].(map[string]any)["url"].(string)
	if got["type"] != "message-edited" || got["message"].(map[string]any)["edited"] != nil || !strings.Contains(link, h.FileLinks.Query("abc")) {
		t.Fatalf("bob: %v", got)
	}
	empty(t, bob)
	empty(t, staff)
	do(carol, `{"type":"file-links","messages":[%d]}`, m.ID)
	empty(t, carol)
}

// A send retry finds its message only in the same chat: a member removed
// from a private room gets no fresh file links by retrying in another one.
func TestRetriesStayInTheirChat(t *testing.T) {
	h, v, _, bob, _ := threadHub(t)
	h.FileLinks = &filelink.Signer{Key: []byte("k"), Now: func() time.Time { return clock }}
	general, secret := h.channels[0].ID, h.channels[1].ID
	do(bob, `{"type":"send","channel":%d,"text":"mine","nonce":"n1"}`, secret)
	sent := nextOf(t, bob, "message")["message"].(map[string]any)["id"]
	drain(h.all()...)
	v.members[2] = false
	do(bob, `{"type":"send","channel":%d,"text":"mine","nonce":"n1"}`, general)
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("got %v (the private message: %v)", m, sent)
	}
}
