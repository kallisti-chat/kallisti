package sidecar

import (
	"kallisti.chat/server/internal/msgrules"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

func TestSendWithANonce(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	alice := joinAs(t, h, mumble.Identity{Session: 1, Name: "alice", Registered: true, UserID: 3})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 4})
	send := `{"type":"send","channel":1,"text":"hi","nonce":"n1"}`
	alice.Handle([]byte(send))
	mine, theirs := next(t, alice), next(t, bob)
	id := mine["message"].(map[string]any)["id"]
	// Only the sender sees the nonce.
	if mine["type"] != "message" || mine["nonce"] != "n1" || theirs["type"] != "message" || theirs["nonce"] != nil {
		t.Fatalf("alice %v, bob %v", mine, theirs)
	}
	// A retry (from a new connection after a reconnect) stores nothing new:
	// the sender gets the message again, nobody else hears of it.
	alice.Close()
	again := joinAs(t, h, mumble.Identity{Session: 5, Name: "alice", Registered: true, UserID: 3})
	again.Handle([]byte(send))
	if m := next(t, again); m["type"] != "message" || m["nonce"] != "n1" || m["message"].(map[string]any)["id"] != id {
		t.Fatalf("retry %v", m)
	}
	empty(t, bob)
	if hist, _ := h.store.History(1, 0, 10); len(hist) != 1 {
		t.Fatalf("stored %d messages", len(hist))
	}
	// Also when slow mode would refuse a new message now.
	h.store.SetSlowMode(1, 60)
	h.reloadChannels()
	again.Handle([]byte(send))
	if m := next(t, again); m["type"] != "message" {
		t.Fatalf("retry in slow mode %v", m)
	}
	// Bob's own nonce of the same value is his.
	bob.Handle([]byte(send))
	if m := next(t, bob); m["type"] != "message" || m["message"].(map[string]any)["id"] == id {
		t.Fatalf("bob %v", m)
	}
}

func TestSendWithoutANonceAsBefore(t *testing.T) {
	h := newHub(t, "general")
	alice, bob := ready(t, h, "alice"), ready(t, h, "bob")
	for range 2 {
		alice.Handle([]byte(`{"type":"send","channel":1,"text":"hi"}`))
		for _, c := range []*Conn{alice, bob} {
			if m := next(t, c); m["type"] != "message" || m["nonce"] != nil {
				t.Fatalf("got %v", m)
			}
		}
	}
	if hist, _ := h.store.History(1, 0, 10); len(hist) != 2 {
		t.Fatalf("stored %d messages", len(hist))
	}
}

func TestLongNoncesAreRefused(t *testing.T) {
	h := newHub(t, "general")
	alice := ready(t, h, "alice")
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"hi","nonce":"` + strings.Repeat("x", maxNonce+1) + `"}`))
	if m := next(t, alice); m["type"] != "error" {
		t.Fatalf("got %v", m)
	}
}

// Every refusal of a send with a nonce carries it: the client fails only
// that send on it.
func TestRefusalsOfASendCarryItsNonce(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	alice := readyAs(t, h, 2, "alice")
	long := strings.Repeat("x", MaxTextLength+1)
	for _, req := range []string{
		`{"type":"send","channel":1,"text":"` + long + `","nonce":"n1"}`,
		`{"type":"send","channel":1,"text":"hi","upload":"nope","nonce":"n1"}`,
		`{"type":"send","channel":999,"text":"hi","nonce":"n1"}`,
	} {
		alice.Handle([]byte(req))
		if m := next(t, alice); m["type"] != "error" || m["nonce"] != "n1" {
			t.Fatalf("%s: %v", req[:40], m)
		}
	}
	// Slow mode.
	h.store.SetSlowMode(1, 60)
	h.reloadChannels()
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"first","nonce":"a"}`))
	next(t, alice)
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"second","nonce":"b"}`))
	if m := next(t, alice); m["about"] != "slow-mode" || m["nonce"] != "b" {
		t.Fatalf("slow mode: %v", m)
	}
	// Other errors, and sends without a nonce, carry none.
	alice.Handle([]byte(`{"type":"history","channel":999}`))
	if m := next(t, alice); m["type"] != "error" || m["nonce"] != nil {
		t.Fatalf("history: %v", m)
	}
	alice.Handle([]byte(`{"type":"send","channel":999,"text":"hi"}`))
	if m := next(t, alice); m["type"] != "error" || m["nonce"] != nil {
		t.Fatalf("no nonce: %v", m)
	}
	// Over the rate limit.
	for range burst {
		alice.Handle([]byte(`{"type":"history","channel":1}`))
		next(t, alice)
	}
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"hi","nonce":"c"}`))
	if m := next(t, alice); m["about"] != "refused" || m["nonce"] != "c" {
		t.Fatalf("rate: %v", m)
	}
}

// Keep the cooldown state, as the built-in server does. A stateless rule
// checker cannot expose retries that incorrectly count as new posts.
type cooldownVoice struct {
	fakeVoice
	last         map[uint32]time.Time
	now          func() time.Time
	allowUploads bool
}

func (v *cooldownVoice) CheckMessage(session uint32, m msgrules.Message) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := clock
	if v.now != nil {
		now = v.now()
	}
	a := msgrules.Author{Since: clock}
	// Allow attachment validation tests to reach the store checks while
	// retaining the stateful post cooldown.
	if v.allowUploads {
		m.Upload = false
	}
	r := msgrules.Rules{NewMinutes: 10, Replacements: []msgrules.Replacement{{Phrase: "grow", With: []string{strings.Repeat("x", MaxTextLength+1)}}}}
	m.Text = msgrules.Replace(r, m.Text, func(int) int { return 0 })
	err := msgrules.Check(r, a, m, now, v.last[session])
	if err == nil && m.Post {
		v.last[session] = now
	}
	return m.Text, err
}

func TestNonceRetryBeforeCooldownAndContent(t *testing.T) {
	h := newHub(t, "general")
	v := &cooldownVoice{last: map[uint32]time.Time{}}
	h.SetVoice(v, "")
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 20})
	do(bob, `{"type":"send","channel":1,"text":"first","nonce":"n1"}`)
	first := next(t, bob)
	if first["type"] != "message" {
		t.Fatalf("first: %v", first)
	}
	for _, content := range []string{`"text":"retry"`, `"poll":{"question":""}`, `"text":"","upload":"missing"`} {
		do(bob, `{"type":"send","channel":1,"nonce":"n1",%s}`, content)
		m := next(t, bob)
		if m["type"] != "message" || m["nonce"] != "n1" || m["message"].(map[string]any)["id"] != first["message"].(map[string]any)["id"] {
			t.Fatalf("retry: %v", m)
		}
	}
	do(bob, `{"type":"send","channel":1,"text":"second","nonce":"n2"}`)
	if m := next(t, bob); m["type"] != "error" || !strings.Contains(m["message"].(string), "5 seconds") {
		t.Fatalf("cooldown: %v", m)
	}
	if hist, err := h.store.History(1, 0, 10); err != nil || len(hist) != 1 {
		t.Fatalf("history: %v %v", hist, err)
	}
}

func TestInvalidContentDoesNotConsumeCooldown(t *testing.T) {
	for _, content := range []string{`"text":""`, `"text":"grow"`, `"text":"hi","reply":999`, `"text":"hi","upload":"missing"`, `"text":"hi","upload":"used"`, `"sticker":999`, `"poll":{"question":""}`} {
		t.Run(content, func(t *testing.T) {
			h := newHub(t, "general")
			v := &cooldownVoice{last: map[uint32]time.Time{}, allowUploads: true}
			up, err := h.store.AddUpload(store.Upload{Key: "used", Name: "a.png", Type: "image/png", Size: 1, Author: "bob"}, clock)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.store.AddMessage(store.NewMessage{ChannelID: 1, Author: "bob", Text: "original", At: clock, Uploads: []store.Attachment{{ID: up.ID}}}); err != nil {
				t.Fatal(err)
			}
			h.SetVoice(v, "")
			bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 20})
			do(bob, `{"type":"send","channel":1,%s}`, content)
			if m := next(t, bob); m["type"] != "error" {
				t.Fatalf("invalid: %v", m)
			}
			do(bob, `{"type":"send","channel":1,"text":"valid","nonce":"ok"}`)
			if m := next(t, bob); m["type"] != "message" {
				t.Fatalf("valid after invalid: %v", m)
			}
		})
	}
}

func TestNonceRetryAfterPrivateAccessRevoked(t *testing.T) {
	h, v, secret := privateHub(t)
	v.members[2] = true
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 20})
	do(bob, `{"type":"send","channel":%d,"text":"private","nonce":"n1"}`, secret)
	if m := next(t, bob); m["type"] != "message" {
		t.Fatalf("first: %v", m)
	}
	v.mu.Lock()
	v.members[2] = false
	v.mu.Unlock()
	do(bob, `{"type":"send","channel":%d,"text":"private","nonce":"n1"}`, secret)
	if m := next(t, bob); m["type"] != "error" || m["message"] != "you may not use this channel's chat" {
		t.Fatalf("revoked: %v", m)
	}
}

func TestCooldownAndSlowModeRefusalsDoNotConsumeOtherLimit(t *testing.T) {
	h := newHub(t, "general", "slow")
	now := clock
	h.now = func() time.Time { return now }
	v := &cooldownVoice{last: map[uint32]time.Time{}, now: func() time.Time { return now }}
	h.SetVoice(v, "")
	if err := h.store.SetSlowMode(2, 60); err != nil {
		t.Fatal(err)
	}
	if err := h.reloadChannels(); err != nil {
		t.Fatal(err)
	}
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 20})
	send := func(channel int64) map[string]any {
		t.Helper()
		do(bob, `{"type":"send","channel":%d,"text":"hi"}`, channel)
		return next(t, bob)
	}
	if m := send(1); m["type"] != "message" {
		t.Fatalf("first A: %v", m)
	}
	if m := send(2); m["type"] != "error" || !strings.Contains(m["message"].(string), "5 seconds") {
		t.Fatalf("cooldown B: %v", m)
	}
	now = now.Add(msgrules.Interval)
	if m := send(2); m["type"] != "message" {
		t.Fatalf("B after cooldown: %v", m)
	}
	now = now.Add(msgrules.Interval)
	if m := send(2); m["about"] != "slow-mode" {
		t.Fatalf("slow B: %v", m)
	}
	if m := send(1); m["type"] != "message" {
		t.Fatalf("A after slow refusal: %v", m)
	}
	for channel, want := range map[int64]int{1: 2, 2: 1} {
		if hist, err := h.store.History(channel, 0, 10); err != nil || len(hist) != want {
			t.Fatalf("channel %d history: %v %v", channel, hist, err)
		}
	}
}
