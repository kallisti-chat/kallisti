package sidecar

import (
	"testing"

	"kallisti.chat/server/internal/mumble"
)

func TestQualityLimits(t *testing.T) {
	h := newHub(t, "general")
	v := &fakeVoice{}
	h.SetVoice(v, "")

	c := h.Connect()
	defer c.Close()
	c.SetIdentity(mumble.Identity{Session: 1, Name: "admin"})
	m := next(t, c)
	if m["type"] != "ready" {
		t.Fatalf("expected ready, got %v", m)
	}
	limits, ok := m["qualityLimits"].(map[string]any)
	if !ok || limits["maxBitrateVoice"] != float64(128_000) {
		t.Fatalf("expected qualityLimits in ready, got %v", m["qualityLimits"])
	}

	admin := c
	bob := readyAs(t, h, 2, "bob")

	// Non-admin cannot set limits
	bob.Handle([]byte(`{"type":"set-quality-limits","qualityLimits":{"maxBitrateVoice":64000,"maxBitrateMusic":128000,"bytesPerSecond":1048576,"maxSide":1080,"maxFps":30}}`))
	if em := next(t, bob); em["type"] != "error" || em["about"] != "manage" {
		t.Fatalf("bob got %v", em)
	}

	// Admin sets limits
	admin.Handle([]byte(`{"type":"set-quality-limits","qualityLimits":{"maxBitrateVoice":64000,"maxBitrateMusic":192000,"bytesPerSecond":2000000,"maxSide":1080,"maxFps":30}}`))
	h.QualityLimitsChanged(QualityLimits{
		MaxBitrateVoice: 64_000,
		MaxBitrateMusic: 192_000,
		BytesPerSecond:  2_000_000,
		MaxSide:         1080,
		MaxFps:          30,
	})

	for _, conn := range []*Conn{admin, bob} {
		msg := next(t, conn)
		if msg["type"] != "quality-limits" {
			t.Fatalf("expected quality-limits, got %v", msg)
		}
	}
}
