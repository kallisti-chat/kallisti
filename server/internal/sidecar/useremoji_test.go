package sidecar

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/time/rate"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

func TestMumbleClientsSeePersonalEmojiAsNames(t *testing.T) {
	h := newHub(t, "general")
	m := store.Message{Text: "nice <:party:12>", Reply: &store.Quote{Author: "bob", Text: "<:cat:3> hi"}}
	if got, want := h.mumbleText(m), "> bob: :cat: hi — nice :party:"; got != want {
		t.Errorf("got %q", got)
	}
}

func addUserEmoji(t *testing.T, h *Hub, user uint32, name, data string) store.UserEmoji {
	t.Helper()
	e, err := h.store.AddUserEmoji(user, name, "image/png", []byte(data), clock)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// myEmoji is a my-emoji event's (or ready's) list as "name=id".
func myEmoji(t *testing.T, m map[string]any) string {
	t.Helper()
	list, ok := m["myEmoji"].([]any)
	if m["type"] == "my-emoji" {
		list, ok = m["emoji"].([]any)
	}
	if !ok {
		t.Fatalf("no list in %v", m)
	}
	var out []string
	for _, e := range list {
		e := e.(map[string]any)
		out = append(out, fmt.Sprintf("%s=%v", e["name"], e["id"]))
	}
	return fmt.Sprint(out)
}

func registered(session, user uint32, name string) mumble.Identity {
	return mumble.Identity{Session: session, Name: name, Registered: true, UserID: user}
}

func TestReadyHasPersonalEmojiAndRemovedImages(t *testing.T) {
	h := newHub(t, "general")
	party := addUserEmoji(t, h, 4, "party", "p")
	gone := addUserEmoji(t, h, 5, "gone", "g")
	h.store.RemoveEmojiImage(gone.ImageID)
	c := h.Connect()
	defer c.Close()
	c.SetIdentity(registered(2, 4, "alice"))
	m := next(t, c)
	if got := myEmoji(t, m); got != fmt.Sprintf("[party=%d]", party.ImageID) {
		t.Fatalf("mine: %s", got)
	}
	if fmt.Sprint(m["removedEmoji"]) != fmt.Sprintf("[%d]", gone.ImageID) {
		t.Fatalf("removed: %v", m["removedEmoji"])
	}
	// Mumble users without an account have none.
	anon := h.Connect()
	defer anon.Close()
	anon.SetIdentity(mumble.Identity{Session: 3, Name: "anon"})
	if got := myEmoji(t, next(t, anon)); got != "[]" {
		t.Fatalf("anon: %s", got)
	}
}

func TestOnlyRegisteredUsersGetPersonalEmojiTokens(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	h.store.CreateAccount(store.Account{Name: "Guest-1", Temporary: true, CertPEM: []byte("c"), KeyPEM: []byte("k")}, clock, "")
	anon := readyAs(t, h, 2, "anon")
	guest := joinAs(t, h, registered(3, 7, "Guest-1"))
	for _, c := range []*Conn{anon, guest} {
		c.Handle([]byte(`{"type":"user-emoji-token","name":"party"}`))
		if m := next(t, c); m["type"] != "error" || m["about"] != "manage" {
			t.Fatalf("got %v", m)
		}
	}
	addEmoji(t, h, "server")
	h.EmojiChanged()
	alice := joinAs(t, h, registered(4, 5, "alice"))
	next(t, guest)
	next(t, anon)
	addUserEmoji(t, h, 5, "mine", "m")
	for _, bad := range []string{"Party", "a", "server", "mine"} {
		alice.Handle([]byte(`{"type":"user-emoji-token","name":"` + bad + `"}`))
		if m := next(t, alice); m["type"] != "error" || m["about"] != "manage" {
			t.Fatalf("%q: %v", bad, m)
		}
	}
	alice.Handle([]byte(`{"type":"user-emoji-token","name":"party"}`))
	m := next(t, alice)
	token, _ := m["token"].(string)
	if m["type"] != "upload-token" || token == "" {
		t.Fatalf("got %v", m)
	}
	// Only for a personal emoji upload, and once.
	if _, ok := h.RedeemUploadToken(token); ok {
		t.Fatal("redeemed as a file upload")
	}
	alice.Handle([]byte(`{"type":"user-emoji-token","name":"party"}`))
	token = next(t, alice)["token"].(string)
	if _, _, ok := h.RedeemEmojiToken(token); ok {
		t.Fatal("redeemed as a server emoji")
	}
	alice.Handle([]byte(`{"type":"user-emoji-token","name":"party"}`))
	token = next(t, alice)["token"].(string)
	if user, name, ok := h.RedeemUserEmojiToken(token); !ok || user != 5 || name != "party" {
		t.Fatalf("redeem: %d %q %v", user, name, ok)
	}
	if _, _, ok := h.RedeemUserEmojiToken(token); ok {
		t.Fatal("redeemed twice")
	}
	if _, _, ok := h.RedeemUserEmojiToken(h.issueUploadToken("alice", "")); ok {
		t.Fatal("a file token redeemed")
	}
}

func TestNewAccountsAddEmojiImagesAsTheyUploadFiles(t *testing.T) {
	h := newHub(t, "general")
	v := &fakeVoice{}
	v.rules.NewMinutes = 10
	v.guests = map[uint32]bool{4: true} // limited as new accounts are
	h.SetVoice(v, "")
	alice := joinAs(t, h, registered(4, 5, "alice"))
	alice.Handle([]byte(`{"type":"user-emoji-token","name":"party"}`))
	if m := next(t, alice); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("got %v", m)
	}
}

func TestPersonalEmojiChangesGoToTheUsersConnections(t *testing.T) {
	h := newHub(t, "general")
	alice := joinAs(t, h, registered(1, 5, "alice"))
	alice.limit = rate.NewLimiter(rate.Inf, 0) // many requests in a row
	phone := joinAs(t, h, registered(2, 5, "alice"))
	bob := joinAs(t, h, registered(3, 6, "bob"))
	img := addUserEmoji(t, h, 6, "cool", "c")
	addEmoji(t, h, "server")
	h.EmojiChanged()
	for _, c := range []*Conn{alice, phone, bob} {
		next(t, c)
	}
	alice.Handle([]byte(`{"type":"add-user-emoji","emoji":"<:cool:` + strconv.FormatInt(img.ImageID, 10) + `>","name":"neat"}`))
	for _, c := range []*Conn{alice, phone} {
		if got := myEmoji(t, next(t, c)); got != fmt.Sprintf("[neat=%d]", img.ImageID) {
			t.Fatalf("after adding: %s", got)
		}
	}
	empty(t, bob)
	// A server emoji becomes a personal one too.
	alice.Handle([]byte(`{"type":"add-user-emoji","emoji":":server:","name":"server_copy"}`))
	m := next(t, alice)
	next(t, phone)
	if got := myEmoji(t, m); got == fmt.Sprintf("[neat=%d]", img.ImageID) {
		t.Fatalf("server emoji not added: %s", got)
	}
	for _, bad := range []string{`"emoji":":nope:","name":"nope"`, `"emoji":"<:x_x:999>","name":"xx"`, `"emoji":"👍","name":"thumb"`,
		`"emoji":"<:cool:` + strconv.FormatInt(img.ImageID, 10) + `>","name":"again"`, `"emoji":":server:","name":"Bad"`} {
		alice.Handle([]byte(`{"type":"add-user-emoji",` + bad + `}`))
		if m := next(t, alice); m["type"] != "error" || m["about"] != "manage" {
			t.Fatalf("%s: %v", bad, m)
		}
	}
	alice.Handle([]byte(`{"type":"rename-user-emoji","name":"neat","to":"wow"}`))
	if got := myEmoji(t, next(t, alice)); !strings.Contains(got, "wow=") || strings.Contains(got, "neat=") {
		t.Fatalf("after renaming: %s", got)
	}
	next(t, phone)
	alice.Handle([]byte(`{"type":"rename-user-emoji","name":"wow","to":"server"}`)) // a server emoji's name
	if m := next(t, alice); m["type"] != "error" {
		t.Fatalf("renamed to a server emoji's name: %v", m)
	}
	for _, name := range []string{"wow", "server_copy"} {
		alice.Handle([]byte(`{"type":"delete-user-emoji","name":"` + name + `"}`))
		next(t, phone)
		if got := myEmoji(t, next(t, alice)); name == "server_copy" && got != "[]" {
			t.Fatalf("after deleting: %s", got)
		}
	}
	// After an upload (files calls it).
	upload := addUserEmoji(t, h, 5, "upload", "u")
	h.UserEmojiChanged(5)
	if got := myEmoji(t, next(t, phone)); got != fmt.Sprintf("[upload=%d]", upload.ImageID) {
		t.Fatalf("after an upload: %s", got)
	}
}

func TestModeratorsRemoveAnEmojiImageEverywhere(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	mod := joinAs(t, h, registered(1, 2, "mod"))
	alice := joinAs(t, h, registered(3, 5, "alice"))
	img := addUserEmoji(t, h, 5, "bad", "b")
	ref := `"<:bad:` + strconv.FormatInt(img.ImageID, 10) + `>"`
	alice.Handle([]byte(`{"type":"remove-emoji-image","emoji":` + ref + `}`))
	if m := next(t, alice); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("not a moderator: %v", m)
	}
	mod.Handle([]byte(`{"type":"remove-emoji-image","emoji":` + ref + `}`))
	for _, c := range []*Conn{mod, alice} {
		m := next(t, c)
		if m["type"] != "removed-emoji" || fmt.Sprint(m["ids"]) != fmt.Sprintf("[%d]", img.ImageID) {
			t.Fatalf("got %v", m)
		}
	}
	if got := myEmoji(t, next(t, alice)); got != "[]" {
		t.Fatalf("alice still has it: %s", got)
	}
	mod.Handle([]byte(`{"type":"remove-emoji-image","emoji":` + ref + `}`))
	if m := next(t, mod); m["type"] != "error" {
		t.Fatalf("removed twice: %v", m)
	}
}

func TestPersonalEmojiReactions(t *testing.T) {
	h := newHub(t, "general")
	alice := joinAs(t, h, registered(1, 4, "alice"))
	img := addUserEmoji(t, h, 9, "party", "p")
	gone := addUserEmoji(t, h, 9, "gone", "g")
	id := say(t, h, alice, "hi")
	ref := func(name string, image int64) string { return "<:" + name + ":" + strconv.FormatInt(image, 10) + ">" }
	// One reaction per image, by the image's first name.
	react(alice, id, ref("mine", img.ImageID), true)
	m := next(t, alice)
	if rs := m["reactions"].([]any); len(rs) != 1 || rs[0].(map[string]any)["emoji"] != ref("party", img.ImageID) {
		t.Fatalf("got %v", m)
	}
	for _, bad := range []string{ref("x_x", 999), ref("gone", gone.ImageID)} {
		if bad == ref("gone", gone.ImageID) {
			react(alice, id, bad, true)
			next(t, alice)
			h.store.RemoveEmojiImage(gone.ImageID)
			react(alice, id, bad, false) // still taken back
			next(t, alice)
		}
		react(alice, id, bad, true)
		if m := next(t, alice); m["type"] != "error" {
			t.Fatalf("%s: %v", bad, m)
		}
	}
}
