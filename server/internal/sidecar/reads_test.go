package sidecar

import (
	"strconv"
	"strings"
	"testing"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

func TestMentionRule(t *testing.T) {
	for text, want := range map[string]bool{
		"@lain hi": true, "hi @Lain!": true, "(@lain)": true,
		"@lainey": false, "mail@lain.example": false, "lain": false,
		"@lain@chat.example": false, // another server's lain
		"@lain.":             true, "@lain@": true,
	} {
		if got := mentionsName(text, "lain"); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
	for text, want := range map[string]bool{
		"hi @lain@chat.example": true, "@Lain@Chat.Example, hi": true, "@lain@chat.example.org": false, "@lain": false,
	} {
		if got := mentionsName(text, "lain@chat.example"); got != want {
			t.Errorf("remote %q: %v", text, got)
		}
	}
}

func TestReadyCarriesReadMarksAndASummary(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	general := h.channels[0].ID
	for _, text := range []string{"old", "@alice old", "new", "@alice new", "@bob not me"} {
		h.store.AddMessage(store.NewMessage{ChannelID: general, Author: "bob", Text: text, At: clock})
	}
	own, _ := h.store.AddMessage(store.NewMessage{ChannelID: general, Author: "alice", Verified: true, AuthorID: 3, Text: "@alice myself", At: clock})
	h.store.SetReadMark(3, general, 2)
	chat, _ := h.store.VoiceChat(5)
	h.store.AddMessage(store.NewMessage{ChannelID: chat, Author: "bob", Text: "@alice in voice", At: clock})

	c := h.Connect()
	t.Cleanup(c.Close)
	c.SetIdentity(mumble.Identity{Session: 1, Name: "Alice", Registered: true, UserID: 3})
	ready := next(t, c)
	reads := ready["reads"].(map[string]any)
	if reads[strconv.FormatInt(general, 10)] != float64(2) {
		t.Fatalf("reads %v", ready["reads"])
	}
	byChannel := map[float64]map[string]any{}
	for _, e := range ready["summary"].([]any) {
		byChannel[e.(map[string]any)["channel"].(float64)] = e.(map[string]any)
	}
	g := byChannel[float64(general)]
	if g["latest"] != float64(own.ID) || g["mentions"] != float64(1) {
		t.Fatalf("general %v", g)
	}
	if v := byChannel[float64(chat)]; v["voice"] != float64(5) || v["mentions"] != float64(1) {
		t.Fatalf("voice chat %v", v)
	}
}

func TestMarkRead(t *testing.T) {
	h := newHub(t, "general")
	general := h.channels[0].ID
	alice := joinAs(t, h, mumble.Identity{Session: 1, Name: "Alice", Registered: true, UserID: 3})
	alice.Handle([]byte(`{"type":"mark-read","channel":` + strconv.FormatInt(general, 10) + `,"message":7}`))
	guest := joinAs(t, h, mumble.Identity{Session: 2, Name: "guest"})
	guest.Handle([]byte(`{"type":"mark-read","channel":` + strconv.FormatInt(general, 10) + `,"message":7}`))
	if m, _ := h.store.ReadMarks(3); m[general] != 7 {
		t.Fatalf("alice marks %v", m)
	}
	if m, _ := h.store.ReadMarks(0); len(m) != 0 { // guests have no id (0 is SuperUser)
		t.Fatalf("guest marks %v", m)
	}
}

func TestMarkReadReachesTheAccountsOtherSessions(t *testing.T) {
	h := newHub(t, "general")
	general := strconv.FormatInt(h.channels[0].ID, 10)
	tab1 := joinAs(t, h, mumble.Identity{Session: 1, Name: "Alice", Registered: true, UserID: 3})
	tab2 := joinAs(t, h, mumble.Identity{Session: 2, Name: "Alice", Registered: true, UserID: 3})
	bob := joinAs(t, h, mumble.Identity{Session: 3, Name: "bob", Registered: true, UserID: 4})
	guest := joinAs(t, h, mumble.Identity{Session: 4, Name: "guest"})
	drain(tab1, tab2, bob, guest)
	tab1.Handle([]byte(`{"type":"mark-read","channel":` + general + `,"message":7}`))
	// The sender too: its read marks must catch up, or it sends them again
	// on every event (and two tabs keep each other busy).
	for _, tab := range []*Conn{tab1, tab2} {
		if m := next(t, tab); m["type"] != "read" || jsonNum(m["channel"]) != general || m["message"] != float64(7) {
			t.Fatalf("tab got %v", m)
		}
	}
	empty(t, bob)
	empty(t, guest)
}

func TestMentionRuleKnowsUnicodeSpaces(t *testing.T) {
	if !mentionsName("hi @lain", "lain") || !mentionsName("　@lain", "lain") {
		t.Fatal("no mention after a no-break or wide space")
	}
}

func TestSearch(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	general := h.channels[0].ID
	h.store.AddMessage(store.NewMessage{ChannelID: general, Author: "alice", Text: "pizza tonight", At: clock})
	secret, _ := h.store.VoiceChat(9) // session 2 may not read channel 9 in the fake
	h.store.AddMessage(store.NewMessage{ChannelID: secret, Author: "alice", Text: "pizza secret", At: clock})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	bob.Handle([]byte(`{"type":"search","text":"pizza"}`))
	m := next(t, bob)
	msgs := m["messages"].([]any)
	if m["type"] != "search-results" || m["text"] != "pizza" || len(msgs) != 1 || msgs[0].(map[string]any)["text"] != "pizza tonight" {
		t.Fatalf("results %v", m)
	}
	bob.Handle([]byte(`{"type":"search","text":"pizza","author":"carol"}`))
	if m := next(t, bob); len(m["messages"].([]any)) != 0 {
		t.Fatalf("by carol: %v", m)
	}
	// Filters without words; a channel the user may not read finds nothing.
	bob.Handle([]byte(`{"type":"search","text":"","author":"alice"}`))
	if m := next(t, bob); len(m["messages"].([]any)) != 1 {
		t.Fatalf("from alice: %v", m)
	}
	bob.Handle([]byte(`{"type":"search","text":"","channel":` + strconv.FormatInt(general, 10) + `}`))
	if m := next(t, bob); len(m["messages"].([]any)) != 1 {
		t.Fatalf("in general: %v", m)
	}
	bob.Handle([]byte(`{"type":"search","text":"pizza","channel":` + strconv.FormatInt(secret, 10) + `}`))
	if m := next(t, bob); len(m["messages"].([]any)) != 0 {
		t.Fatalf("in a secret chat: %v", m)
	}
	for _, text := range []string{" ", "!!!"} {
		bob.Handle([]byte(`{"type":"search","text":"` + text + `"}`))
		if m := next(t, bob); len(m["messages"].([]any)) != 0 {
			t.Fatalf("nothing (%q): %v", text, m)
		}
	}
}

func TestSearchTextHasALimit(t *testing.T) {
	h := newHub(t, "general")
	c := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	c.Handle([]byte(`{"type":"search","text":"` + strings.Repeat("x", 201) + `"}`))
	if m := next(t, c); m["type"] != "error" {
		t.Fatalf("long search: %v", m)
	}
}

// Mark unread: the mark goes back (to the message before), the account's
// sessions follow; guests and chats one may not read: nothing.
func TestMarkUnread(t *testing.T) {
	h := newHub(t, "general")
	general := strconv.FormatInt(h.channels[0].ID, 10)
	tab1 := joinAs(t, h, mumble.Identity{Session: 1, Name: "Alice", Registered: true, UserID: 3})
	tab2 := joinAs(t, h, mumble.Identity{Session: 2, Name: "Alice", Registered: true, UserID: 3})
	guest := joinAs(t, h, mumble.Identity{Session: 4, Name: "guest"})
	if err := h.store.SetReadMark(3, h.channels[0].ID, 20); err != nil {
		t.Fatal(err)
	}
	drain(tab1, tab2, guest)
	tab1.Handle([]byte(`{"type":"mark-unread","channel":` + general + `,"message":12}`))
	for _, tab := range []*Conn{tab1, tab2} {
		if m := next(t, tab); m["type"] != "read" || jsonNum(m["channel"]) != general || m["message"] != float64(11) || m["back"] != true {
			t.Fatalf("tab got %v", m)
		}
	}
	if m, _ := h.store.ReadMarks(3); m[h.channels[0].ID] != 11 {
		t.Fatalf("marks %v", m)
	}
	guest.Handle([]byte(`{"type":"mark-unread","channel":` + general + `,"message":12}`))
	tab1.Handle([]byte(`{"type":"mark-unread","channel":99999,"message":12}`))
	empty(t, guest)
	empty(t, tab1)
	if m, _ := h.store.ReadMarks(0); len(m) != 0 {
		t.Fatalf("guest marks %v", m)
	}
}
