package sidecar

import (
	"fmt"
	"testing"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// BenchmarkReadStateFarBehind is the summary of a user who is far behind in
// many channels: every message after the read mark is looked at.
func BenchmarkReadStateFarBehind(b *testing.B) {
	const channels, perChannel = 40, 600
	names := make([]string, channels)
	for i := range names {
		names[i] = fmt.Sprintf("chan%d", i)
	}
	h := newHub(b, names...)
	for _, ch := range h.channels {
		for i := 0; i < perChannel; i++ {
			m := store.NewMessage{ChannelID: ch.ID, Author: "bob", Text: "hello there", At: clock}
			if i%3 == 0 {
				m.Text = "@alice look at this"
			}
			if i%10 == 0 { // some with a file, so the uploads step has work
				u, err := h.store.AddUpload(store.Upload{Key: fmt.Sprintf("k%d-%d", ch.ID, i), Name: "f.png", Type: "image/png", Size: 1, Author: "bob"}, clock)
				if err != nil {
					b.Fatal(err)
				}
				m.Uploads = []store.Attachment{{ID: u.ID}}
			}
			if _, err := h.store.AddMessage(m); err != nil {
				b.Fatal(err)
			}
		}
	}
	id := mumble.Identity{Session: 1, Name: "Alice", Registered: true, UserID: 3}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, summary := h.readState(id); len(summary) != channels {
			b.Fatalf("summary of %d channels", len(summary))
		}
	}
}
