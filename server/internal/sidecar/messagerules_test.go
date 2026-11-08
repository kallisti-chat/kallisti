package sidecar

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/msgrules"
	"kallisti.chat/server/internal/mumble"
)

// Session 1 is an admin (staff); sessions in guests are guests.
// Word replacements take the first replacement.
func (f *fakeVoice) CheckMessage(session uint32, m msgrules.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, m)
	m.Text = msgrules.Replace(f.rules, m.Text, func(int) int { return 0 })
	return m.Text, msgrules.Check(f.rules, msgrules.Author{Staff: session == 1, Guest: f.guests[session]}, m, clock, time.Time{})
}

func (f *fakeVoice) MessageRules(session uint32) (msgrules.Rules, error) {
	if session != 1 {
		return msgrules.Rules{}, errors.New("only admins see the message rules")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rules, nil
}

func (f *fakeVoice) SetMessageRules(session uint32, r msgrules.Rules) (msgrules.Rules, error) {
	if session != 1 {
		return msgrules.Rules{}, errors.New("only admins change the message rules")
	}
	r, err := msgrules.Normalize(r)
	if err == nil {
		f.mu.Lock()
		f.rules = r
		f.mu.Unlock()
	}
	return r, err
}

func (f *fakeVoice) lastChecked() msgrules.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checked[len(f.checked)-1]
}

func TestMessageRulesAreForAdmins(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	admin := readyAs(t, h, 1, "admin")
	bob := readyAs(t, h, 2, "bob")
	for _, req := range []string{`{"type":"message-rules"}`, `{"type":"set-message-rules","rules":{"words":["x"]}}`} {
		bob.Handle([]byte(req))
		if m := next(t, bob); m["type"] != "error" || m["about"] != "manage" {
			t.Fatalf("%s: bob got %v", req, m)
		}
	}
	admin.Handle([]byte(`{"type":"set-message-rules","rules":{"newMinutes":10,"words":[" Spam "],"domains":["https://Junk.example/x"]}}`))
	m := next(t, admin)
	r, _ := m["rules"].(map[string]any)
	if m["type"] != "message-rules" || r["newMinutes"] != 10.0 || r["words"].([]any)[0] != "Spam" || r["domains"].([]any)[0] != "junk.example" {
		t.Fatalf("admin got %v", m)
	}
	admin.Handle([]byte(`{"type":"message-rules"}`))
	if m := next(t, admin); m["type"] != "message-rules" || m["rules"].(map[string]any)["newMinutes"] != 10.0 {
		t.Fatalf("admin got %v", m)
	}
	admin.Handle([]byte(`{"type":"set-message-rules","rules":{"domains":["no domain"]}}`))
	if m := next(t, admin); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("admin got %v", m)
	}
}

func TestMessageRulesOnSendEditAndUpload(t *testing.T) {
	h := newHub(t, "general")
	v := &fakeVoice{guests: map[uint32]bool{3: true}, rules: msgrules.Rules{NewMinutes: 10, Words: []string{"spam"}}}
	h.SetVoice(v, "")
	admin := readyAs(t, h, 1, "admin")
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 20})
	guest := readyAs(t, h, 3, "guest")
	ch := strconv.FormatInt(h.channels[0].ID, 10)
	refused := func(c *Conn, req, want string) {
		t.Helper()
		c.Handle([]byte(req))
		if m := next(t, c); m["type"] != "error" || m["message"] != want {
			t.Fatalf("%s: %v, want %q", req, m, want)
		}
	}

	// A refused send comes back with its text, for the composer.
	bob.Handle([]byte(`{"type":"send","channel":` + ch + `,"text":"buy spam"}`))
	if m := next(t, bob); m["message"] != "This message contains a blocked word" || m["about"] != "refused" ||
		m["text"] != "buy spam" || m["channel"] != float64(h.channels[0].ID) {
		t.Fatalf("send: %v", m)
	}
	refused(guest, `{"type":"send","channel":`+ch+`,"text":"see https://x.example"}`, "Guests cannot post links: log in or make an account")
	refused(guest, `{"type":"upload-token"}`, "Guests cannot upload files: log in or make an account")
	if got := v.lastChecked(); got != (msgrules.Message{Upload: true}) {
		t.Fatalf("upload token: %+v", got)
	}
	refused(guest, `{"type":"send","channel":`+ch+`,"text":"hi @everyone"}`, "Guests cannot mention @everyone or @here: log in or make an account")
	refused(guest, `{"type":"send","channel":`+ch+`,"text":"a caption","upload":"key"}`, "Guests cannot upload files: log in or make an account")
	if got := v.lastChecked(); got != (msgrules.Message{Text: "a caption", Upload: true}) {
		t.Fatalf("send: %+v", got)
	}

	// Edits are checked too, without the rate.
	id := strconv.FormatInt(say(t, h, bob, "hello"), 10)
	next(t, admin)
	next(t, guest)
	refused(bob, `{"type":"edit-message","message":`+id+`,"text":"hello spam"}`, "This message contains a blocked word")
	if got := v.lastChecked(); got != (msgrules.Message{Text: "hello spam"}) {
		t.Fatalf("edit: %+v", got)
	}
	if m, _ := h.store.MessageByID(mustID(t, id)); m.Text != "hello" {
		t.Fatalf("edited to %q", m.Text)
	}

	// Staff are exempt.
	say(t, h, admin, "spam @everyone https://x.example")
	if !slices.ContainsFunc(v.checked, func(m msgrules.Message) bool { return m.MentionAll }) {
		t.Fatal("no mention seen")
	}
}

func TestWordReplacementsOnSendAndEdit(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{rules: msgrules.Rules{Replacements: []msgrules.Replacement{{Phrase: "nice ass", With: []string{"beautiful behind"}}}}}, "")
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 20})
	ch := strconv.FormatInt(h.channels[0].ID, 10)
	// The author gets the posted text like everyone.
	bob.Handle([]byte(`{"type":"send","channel":` + ch + `,"text":"what a Nice a s s"}`))
	m := next(t, bob)
	if m["type"] != "message" || m["message"].(map[string]any)["text"] != "what a Beautiful behind" {
		t.Fatalf("send: %v", m)
	}
	id := strconv.FormatInt(int64(m["message"].(map[string]any)["id"].(float64)), 10)
	bob.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"so nice ass"}`))
	next(t, bob)
	if m, _ := h.store.MessageByID(mustID(t, id)); m.Text != "so beautiful behind" {
		t.Fatalf("edited to %q", m.Text)
	}
	// Too long with the replacement: refused, the text back to the composer.
	long := strings.Repeat("x", MaxTextLength-9) + " nice ass"
	bob.Handle([]byte(`{"type":"send","channel":` + ch + `,"text":"` + long + `"}`))
	if m := next(t, bob); m["about"] != "refused" || m["text"] != long || !strings.Contains(m["message"].(string), "too long") {
		t.Fatalf("long: %v", m)
	}
}

func mustID(t *testing.T, s string) int64 {
	t.Helper()
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// No rules saved yet: empty lists, not null (the client wants lists).
func TestMessageRulesSendsEmptyLists(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	admin := readyAs(t, h, 1, "admin")
	admin.Handle([]byte(`{"type":"message-rules"}`))
	r := next(t, admin)["rules"].(map[string]any)
	if _, ok := r["words"].([]any); !ok {
		t.Fatalf("words %v", r["words"])
	}
	if _, ok := r["domains"].([]any); !ok {
		t.Fatalf("domains %v", r["domains"])
	}
	if _, ok := r["replacements"].([]any); !ok {
		t.Fatalf("replacements %v", r["replacements"])
	}
}
