package sidecar

import (
	"testing"

	"kallisti.chat/server/internal/mumble"
)

func TestStreamTokensAndLiveStreams(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	h.StreamToken = func(session uint32) string { return "token-for-" + string(rune('0'+session)) }
	h.LiveStreams = func() []LiveStream { return []LiveStream{{Session: 4, Channel: 2}} }
	c := h.Connect()
	t.Cleanup(c.Close)
	c.SetIdentity(mumble.Identity{Session: 3, Name: "alice"})
	ready := next(t, c)
	if s := ready["streams"].([]any); len(s) != 1 || s[0].(map[string]any)["session"] != float64(4) {
		t.Fatalf("ready %v", ready)
	}
	c.Handle([]byte(`{"type":"stream-token"}`))
	if m := next(t, c); m["type"] != "stream-token" || m["token"] != "token-for-3" {
		t.Fatalf("token %v", m)
	}
	h.StreamsChanged([]LiveStream{})
	if m := next(t, c); m["type"] != "streams" || len(m["streams"].([]any)) != 0 {
		t.Fatalf("streams %v", m)
	}
}

// Previews only reach people who may enter the stream's channel (session 2
// may not read channel 9 in the fake).
func TestStreamPreviewsFollowChannelAccess(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	h.LiveStreams = func() []LiveStream {
		return []LiveStream{{Session: 4, Channel: 9, Preview: "data:image/jpeg;base64,AA"}}
	}
	may := readyAs(t, h, 1, "may")
	mayNot := h.Connect()
	t.Cleanup(mayNot.Close)
	mayNot.SetIdentity(mumble.Identity{Session: 2, Name: "maynot"})
	ready := next(t, mayNot)
	if s := ready["streams"].([]any)[0].(map[string]any); s["preview"] != nil {
		t.Fatalf("preview for someone who may not enter: %v", s)
	}
	h.StreamPreview(LiveStream{Session: 4, Channel: 9, Preview: "data:image/jpeg;base64,BB"})
	if m := next(t, may); m["type"] != "stream-preview" || m["image"] != "data:image/jpeg;base64,BB" {
		t.Fatalf("preview %v", m)
	}
	empty(t, mayNot)
	h.StreamsChanged([]LiveStream{{Session: 4, Channel: 9, Preview: "data:image/jpeg;base64,BB"}})
	if s := next(t, mayNot)["streams"].([]any)[0].(map[string]any); s["preview"] != nil {
		t.Fatalf("preview in the list for all: %v", s)
	}
}

// A notice (a problem of an OBS stream) reaches only its owner's session.
func TestNoticeReachesOneSession(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	owner, other := readyAs(t, h, 1, "owner"), readyAs(t, h, 3, "other")
	h.Notice(1, "B-frames")
	if m := next(t, owner); m["type"] != "notice" || m["message"] != "B-frames" {
		t.Fatalf("notice %v", m)
	}
	empty(t, other)
}
