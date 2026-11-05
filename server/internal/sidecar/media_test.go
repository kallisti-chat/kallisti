package sidecar

import (
	"fmt"
	"testing"

	"kallisti.chat/server/internal/store"
)

func TestUploadAccessResolvesContainingRoom(t *testing.T) {
	h, _, staff, _, _ := threadHub(t)
	secret := h.channels[1].ID
	thread := startThread(t, h, staff, postTo(t, h, staff, secret, "pictures"), "Food")
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
	for _, channel := range []int64{secret, thread} {
		key := attach(channel)
		for _, allowed := range []bool{false, true} {
			called := false
			got := h.MayReadUpload(key, func(mirror uint32, text bool) bool {
				called = true
				if mirror != 101 || !text {
					t.Fatalf("wrong parent room: %d text=%v", mirror, text)
				}
				return allowed
			})
			if !called || got != allowed {
				t.Fatalf("room policy not applied: got %v", got)
			}
		}
	}
	u, err := h.store.AddUpload(store.Upload{Key: "unattached", Author: "mod"}, clock)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{u.Key, "missing"} {
		if h.MayReadUpload(key, func(uint32, bool) bool { t.Fatal("no containing room"); return true }) {
			t.Fatal("unattached upload was public")
		}
	}
	if _, err := h.store.DeleteThread(thread); err != nil {
		t.Fatal(err)
	}
	if h.MayReadUpload(fmt.Sprintf("upload-%d", thread), func(uint32, bool) bool { return true }) {
		t.Fatal("deleted thread was readable")
	}
}
