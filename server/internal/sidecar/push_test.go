package sidecar

import (
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/push"
	"kallisti.chat/server/internal/store"
)

// A thread's push names its channel: devices keep the threads of their
// quiet channels quiet (see push.Note).
func TestThreadPushesNameTheChannel(t *testing.T) {
	h, _, mod, _, _ := pushHub(t)
	general := h.channels[0].ID
	id := startThread(t, h, mod, postTo(t, h, mod, general, "topic"), "Topic")
	var got []push.Note
	h.Push = func(_ string, n push.Note) { got = append(got, n) }
	postTo(t, h, mod, id, "@alice look")
	if len(got) != 1 || got[0].Channel != id || got[0].Parent != general {
		t.Fatalf("got %+v", got)
	}
}

// Offline access: private mirrors only for bob (20), as Private says;
// voice channel 9 for nobody.
func (f *fakeVoice) UserSees(user, channel uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.hidden[channel] || user == 20
}
func (f *fakeVoice) UserMayRead(user, channel uint32) bool { return channel != 9 }
func (f *fakeVoice) UserBanned(user uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.banned[user]
}

func TestPings(t *testing.T) {
	alice := uint32(40)
	from := func(m store.Message) store.Message {
		m.Author, m.Verified, m.AuthorID = "bob", true, new(uint32(20))
		return m
	}
	for _, c := range []struct {
		m      store.Message
		direct bool
		want   bool
	}{
		{from(store.Message{Text: "hi @Alice"}), false, true},
		{from(store.Message{Text: "hi @alicex"}), false, false},
		{from(store.Message{Text: "hi alice"}), false, false},
		{from(store.Message{Text: "hi"}), true, true}, // a direct message
		{from(store.Message{Text: "@everyone", MentionAll: "everyone"}), false, true},
		{from(store.Message{Text: "@here", MentionAll: "here"}), false, false}, // for who is there
		{from(store.Message{Text: "@everyone"}), false, false},                 // not by a moderator
		{from(store.Message{Text: "yes", Reply: &store.Quote{Author: "alice"}}), false, true},
		{from(store.Message{Text: "yes", Reply: &store.Quote{Author: "alice"}, NoPing: true}), false, false},
		{from(store.Message{Text: "yes", Reply: &store.Quote{Author: ""}}), false, false},                     // deleted since
		{from(store.Message{Text: "yes", Reply: &store.Quote{Author: "alice", Webhook: true}}), false, false}, // a webhook's post named so
		{store.Message{Author: "alice", Verified: true, AuthorID: &alice, Text: "@alice"}, true, false},
		{store.Message{Author: "Alice", Text: "@alice me"}, false, false}, // own (no id)
	} {
		if got := pings(c.m, c.direct, "alice", alice); got != c.want {
			t.Errorf("%+v direct %v: got %v", c.m, c.direct, got)
		}
	}
}

func TestWantsPush(t *testing.T) {
	for _, c := range []struct {
		t    pushTarget
		want bool
	}{
		{pushTarget{mayRead: true}, true},
		{pushTarget{mayRead: true, online: true}, false}, // the app tells it
		{pushTarget{mayRead: true, dnd: true}, false},
		{pushTarget{mayRead: true, banned: true}, false},
		{pushTarget{}, false},
	} {
		if got := wantsPush(c.t); got != c.want {
			t.Errorf("%+v: got %v", c.t, got)
		}
	}
}

// pushHub: privateHub, with registered users mod (10, session 1, online),
// bob (20), carol (30, session 3, online) and alice (40); all but mod and
// carol have push subscriptions. It returns the pushes so far.
func pushHub(t *testing.T) (*Hub, *fakeVoice, *Conn, *Conn, func() []string) {
	t.Helper()
	h, v, _ := privateHub(t)
	v.names = map[uint32]string{10: "mod", 20: "bob", 30: "carol", 40: "alice"}
	v.online = map[uint32]OnlineUser{10: {"mod", 1}, 30: {"carol", 3}}
	for i, name := range []string{"bob", "alice", "mod"} {
		acc, err := h.store.CreateAccount(store.Account{Name: name, PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, clock, "")
		if err != nil {
			t.Fatal(err)
		}
		session := fmt.Sprint("s", i)
		h.store.CreateSession(session, acc.ID, clock.Add(time.Hour))
		h.store.SetPushSubscription(session, store.PushSubscription{Endpoint: "https://fcm.googleapis.com/" + name, P256dh: "p", Auth: "a"}, clock)
	}
	var mu sync.Mutex
	var got []string
	h.Push = func(name string, n push.Note) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, fmt.Sprintf("%s: %s %s", name, n.Body, n.URL))
		if n.Author == "" {
			t.Errorf("no author for blocks: %+v", n)
		}
	}
	mod := joinAs(t, h, mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 10})
	carol := joinAs(t, h, mumble.Identity{Session: 3, Name: "carol", Registered: true, UserID: 30})
	return h, v, mod, carol, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		slices.Sort(out)
		return out
	}
}

// post sends a message and returns its URL path (the readers' copies are
// read and dropped).
func post(t *testing.T, h *Hub, c *Conn, channel int64, extra string) string {
	t.Helper()
	ch := strconv.FormatInt(channel, 10)
	c.Handle([]byte(`{"type":"send","channel":` + ch + `,` + extra + `}`))
	for m := next(t, c); ; m = next(t, c) {
		if m["type"] == "message" {
			drain(h.all()...)
			return fmt.Sprintf("/channels/%s/%d", ch, int64(m["message"].(map[string]any)["id"].(float64)))
		}
		if m["type"] == "error" {
			t.Fatalf("error %v", m)
		}
	}
}

func TestPushes(t *testing.T) {
	h, v, mod, carol, pushes := pushHub(t)
	general, secret := h.channels[0].ID, h.channels[1].ID
	check := func(what string, want ...string) {
		t.Helper()
		if got := pushes(); !slices.Equal(got, want) {
			t.Errorf("%s: got %q, want %q", what, got, want)
		}
	}

	url := post(t, h, mod, general, `"text":"@alice @bob @carol hi"`)
	check("mentions", "alice: Mention in #general from mod "+url, "bob: Mention in #general from mod "+url)

	v.setOnline(map[uint32]OnlineUser{10: {"mod", 1}, 30: {"carol", 3}, 20: {"bob", 2}})
	url = post(t, h, mod, general, `"text":"@alice @bob again"`)
	check("connected users get none", "alice: Mention in #general from mod "+url)
	v.setOnline(map[uint32]OnlineUser{10: {"mod", 1}, 30: {"carol", 3}})

	h.store.SetUserStatus(40, store.UserStatus{Status: "dnd"})
	post(t, h, mod, general, `"text":"@alice dnd?"`)
	check("do not disturb")
	h.store.SetUserStatus(40, store.UserStatus{})

	url = post(t, h, mod, secret, `"text":"@everyone @alice the plan"`)
	check("private channel: members only", "bob: Mention in #secret from mod "+url)
	post(t, h, mod, general, `"text":"@here quick"`)
	check("@here is for who is there")
	post(t, h, carol, general, `"text":"@everyone spam"`)
	check("@everyone needs a moderator")

	orig, _ := h.store.AddMessage(store.NewMessage{ChannelID: general, Author: "alice", Verified: true, AuthorID: 40, Text: "lunch?", At: clock})
	url = post(t, h, carol, general, `"text":"yes","reply":`+strconv.FormatInt(orig.ID, 10))
	check("reply", "alice: Mention in #general from carol "+url)
	post(t, h, carol, general, `"text":"yes","reply":`+strconv.FormatInt(orig.ID, 10)+`,"noPing":true`)
	check("reply without ping")

	dm, _ := strconv.ParseInt(openDirect(t, carol, 40), 10, 64)
	url = post(t, h, carol, dm, `"text":"psst"`)
	check("direct message", "alice: Message from carol "+url)

	v.mu.Lock()
	v.banned = map[uint32]bool{40: true}
	v.mu.Unlock()
	post(t, h, carol, dm, `"text":"still there?"`)
	url = post(t, h, mod, general, `"text":"@alice @bob banned?"`)
	check("banned users get none", "bob: Mention in #general from mod "+url)
	v.mu.Lock()
	v.banned = nil
	v.mu.Unlock()

	h.MumblePrivateText(mumble.Identity{Session: 7, Name: "carol", Registered: true, UserID: 30}, 40, "from Mumble", nil, false)
	if got := pushes(); len(got) != 1 || got[0][:25] != "alice: Message from carol" {
		t.Errorf("from a Mumble client: %q", got)
	}

	for voice, want := range map[uint32]string{5: "alice: Mention in a voice channel chat from mod", 9: ""} {
		h.MumbleText(mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 10}, voice, "@alice", nil)
		drain(h.all()...)
		if got := pushes(); want == "" && len(got) != 0 || want != "" && (len(got) != 1 || got[0][:len(want)] != want) {
			t.Errorf("voice chat %d: %q", voice, got)
		}
	}
}
