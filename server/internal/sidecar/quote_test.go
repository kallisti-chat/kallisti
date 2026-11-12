package sidecar

import (
	"testing"

	"kallisti.chat/server/internal/store"
)

// A reply's quote shows a small picture of the answered message's first
// file: its preview, else a picture browsers show; never a spoiler.
func TestQuoteImage(t *testing.T) {
	h := &Hub{}
	for _, c := range []struct {
		name string
		u    *store.Upload
		want string
	}{
		{"none", nil, ""},
		{"picture", &store.Upload{Key: "k", Name: "cat.png", Type: "image/png"}, "/files/k/cat.png"},
		{"preview", &store.Upload{Key: "k", Name: "clip.mp4", Type: "video/mp4", Preview: "image/webp"}, "/files/k/clip.mp4?preview"},
		{"GIF with a video preview", &store.Upload{Key: "k", Name: "a.gif", Type: "image/gif", Preview: "video/mp4"}, "/files/k/a.gif"},
		{"spoiler", &store.Upload{Key: "k", Name: "SPOILER_cat.png", Type: "image/png"}, ""},
		{"spoiler with preview", &store.Upload{Key: "k", Name: "SPOILER_clip.mp4", Type: "video/mp4", Preview: "image/webp"}, ""},
		{"file", &store.Upload{Key: "k", Name: "notes.pdf", Type: "application/pdf"}, ""},
		{"picture browsers cannot show", &store.Upload{Key: "k", Name: "x.jxl", Type: "image/jxl"}, ""},
	} {
		if got := h.quoteImage(c.u); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
