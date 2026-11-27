package sidecar

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kallisti.chat/server/internal/push"
	"kallisti.chat/server/internal/store"
)

// subscribe makes an account with a push subscription (all: its chats on
// "All messages").
func subscribe(tb testing.TB, h *Hub, name string, all ...int64) {
	tb.Helper()
	acc, err := h.store.CreateAccount(store.Account{Name: name, PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, clock, "")
	if err != nil {
		tb.Fatal(err)
	}
	session := "s-" + name
	if err := h.store.CreateSession(session, acc.ID, clock.Add(time.Hour)); err != nil {
		tb.Fatal(err)
	}
	if err := h.store.SetPushSubscription(session, store.PushSubscription{Endpoint: "https://fcm.googleapis.com/" + name, P256dh: "p", Auth: "a", All: all}, clock); err != nil {
		tb.Fatal(err)
	}
}

// pushUsersHub is privateHub (general, and secret which only bob reads)
// with subscribed accounts for the registered users in users (by account
// name; PushUser finds them, with a database lookup as the server's
// does). mod is connected. It returns the pushes so far, as "name: body
// all=…", sorted, and how many PushUser calls.
func pushUsersHub(tb testing.TB, users map[string]uint32, all map[string]bool) (*Hub, *fakeVoice, func() []string, *atomic.Int64) {
	tb.Helper()
	h := newHub(tb, "general", "secret")
	v := &fakeVoice{hidden: map[uint32]bool{101: true}, members: map[uint32]bool{}}
	h.SetVoice(v, "")
	v.names = map[uint32]string{}
	v.online = map[uint32]OnlineUser{10: {"mod", 1}}
	general := h.channels[0].ID
	for _, name := range slices.Sorted(maps.Keys(users)) {
		v.names[users[name]] = name
		if all[name] {
			subscribe(tb, h, name, general)
		} else {
			subscribe(tb, h, name)
		}
	}
	var calls atomic.Int64
	h.PushUser = func(name string) (uint32, bool) {
		calls.Add(1)
		// One of the server's two lookups, for its cost.
		if _, err := h.store.AccountByName(name); err != nil {
			return 0, false
		}
		u, ok := users[name]
		return u, ok
	}
	var mu sync.Mutex
	var got []string
	h.Push = func(name string, n push.Note) {
		mu.Lock()
		defer mu.Unlock()
		if n.Allowed == nil || !n.Allowed() {
			tb.Errorf("%s: not allowed when queued: %+v", name, n)
		}
		got = append(got, fmt.Sprintf("%s: %s all=%v", name, n.Body, n.All))
	}
	return h, v, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		slices.Sort(out)
		return out
	}, &calls
}

// Who gets a push for a message, and which, in many cases at once.
func TestPushTargets(t *testing.T) {
	users := map[string]uint32{
		"mod": 10, "bob": 20, "alice": 40, "al": 41, "alice.b": 42, "Zoë": 43, "kai": 44,
		"frank": 45, "hank": 46, "ivy": 47, "kim": 50,
	}
	h, v, pushes, _ := pushUsersHub(t, users, map[string]bool{"ivy": true, "kim": true})
	// dave: an account whose certificate is not the registration's.
	v.names[60] = "dave"
	subscribe(t, h, "dave")
	v.names[30] = "carol" // no account
	v.banned = map[uint32]bool{45: true}
	h.store.SetUserStatus(46, store.UserStatus{Status: "dnd"})

	general, secret := chat{id: h.channels[0].ID}, chat{id: h.channels[1].ID}
	dm := func(a, b uint32) chat {
		id, err := h.store.DirectChat(a, b)
		if err != nil {
			t.Fatal(err)
		}
		return chat{id: id, direct: true, peers: [2]uint32{min(a, b), max(a, b)}}
	}
	kim := func(m store.Message) store.Message {
		m.Author, m.Verified, m.AuthorID = "kim", true, new(uint32(50))
		return m
	}
	alice := func(m store.Message) store.Message {
		m.Author, m.Verified, m.AuthorID = "alice", true, new(uint32(40))
		return m
	}
	text := func(s string) store.Message { return kim(store.Message{Text: s}) }
	reply := func(to string, noPing, webhook bool) store.Message {
		return kim(store.Message{Text: "yes", Reply: &store.Quote{Author: to, Webhook: webhook}, NoPing: noPing})
	}
	everyone := kim(store.Message{Text: "@everyone look", MentionAll: "everyone"})
	const (
		ivyAll      = "ivy: Message in #general from kim all=true"
		mention     = ": Mention in #general from kim all=false"
		fromKim     = ": Message from kim all=false"
		voiceChat   = ": Mention in a voice channel chat from kim all=false"
		secretPing  = ": Mention in #secret from kim all=false"
		mentionIvy  = "ivy" + mention
		mentionBob  = "bob" + mention
		mentionAli  = "alice" + mention
		mentionAl   = "al" + mention
		mentionAliB = "alice.b" + mention
	)
	for _, c := range []struct {
		what string
		c    chat
		m    store.Message
		want []string
	}{
		{"plain", general, text("lunch?"), []string{ivyAll}},
		{"plain in secret", secret, text("lunch?"), nil},
		{"mention", general, text("@alice hi"), []string{mentionAli, ivyAll}},
		{"mention, other case", general, text("hi @ALICE"), []string{mentionAli, ivyAll}},
		{"a shorter name", general, text("@al hi"), []string{mentionAl, ivyAll}},
		{"a longer name", general, text("@alice.b hi"), []string{mentionAliB, ivyAll}},
		{"names ending a sentence", general, text("(@alice. and @al,"), []string{mentionAl, mentionAli, ivyAll}},
		{"unicode", general, text("@zoë hi"), []string{"Zoë" + mention, ivyAll}},
		{"unicode, other case", general, text("@ZOË hi"), []string{"Zoë" + mention, ivyAll}},
		{"kelvin sign", general, text("@Kai hi"), []string{"kai" + mention, ivyAll}},
		{"another server's account", general, text("@alice@example.org hi"), []string{ivyAll}},
		{"not after a space", general, text("x@alice hi"), []string{ivyAll}},
		{"no @", general, text("alice hi"), []string{ivyAll}},
		{"self mention", general, text("@kim hi"), []string{ivyAll}},
		{"all messages user mentioned", general, text("@ivy hi"), []string{mentionIvy}},
		{"connected", general, text("@mod hi"), []string{ivyAll}},
		{"do not disturb", general, text("@hank hi"), []string{ivyAll}},
		{"banned", general, text("@frank hi"), []string{ivyAll}},
		{"account not the registration", general, text("@dave hi"), []string{ivyAll}},
		{"no account", general, text("@carol hi"), []string{ivyAll}},
		{"many", general, text("@alice @bob @mod @hank @frank @dave @kim hi"), []string{mentionAli, mentionBob, ivyAll}},
		{"may not read", secret, text("@alice hi"), nil},
		{"may read", secret, text("@bob hi"), []string{"bob" + secretPing}},
		{"everyone", general, everyone, []string{"Zoë" + mention, mentionAl, mentionAliB, mentionAli, mentionBob, mentionIvy, "kai" + mention}},
		{"everyone in secret", secret, everyone, []string{"bob" + secretPing}},
		{"here", general, kim(store.Message{Text: "@here look", MentionAll: "here"}), []string{ivyAll}},
		{"everyone not by a moderator", general, text("@everyone look"), []string{ivyAll}},
		{"reply", general, reply("alice", false, false), []string{mentionAli, ivyAll}},
		{"reply, other case", general, reply("ALICE", false, false), []string{mentionAli, ivyAll}},
		{"reply without ping", general, reply("alice", true, false), []string{ivyAll}},
		{"reply to a webhook", general, reply("alice", false, true), []string{ivyAll}},
		{"reply to a deleted message", general, reply("", false, false), []string{ivyAll}},
		{"reply to self", general, reply("kim", false, false), []string{ivyAll}},
		{"reply without ping, mention", general, kim(store.Message{Text: "@al yes", Reply: &store.Quote{Author: "alice"}, NoPing: true}), []string{mentionAl, ivyAll}},
		{"own by name", general, store.Message{Author: "IVY", Text: "lunch?"}, []string{"kim: Message in #general from IVY all=true"}},
		{"own by name, mention", general, store.Message{Author: "Kim", Text: "@ivy @kim"}, []string{"ivy: Mention in #general from Kim all=false"}},
		{"direct", dm(40, 50), text("psst"), []string{"alice" + fromKim}},
		{"direct, back", dm(40, 50), alice(store.Message{Text: "psst"}), []string{"kim: Message from alice all=false"}},
		{"direct, mentions", dm(40, 50), kim(store.Message{Text: "@al @bob @everyone", MentionAll: "everyone"}), []string{"alice" + fromKim}},
		{"direct, reply", dm(40, 50), reply("alice", true, false), []string{"alice" + fromKim}},
		{"direct, peer not subscribed", dm(30, 50), text("psst"), nil},
		{"direct, peer connected", dm(10, 50), text("psst"), nil},
		{"direct, peer banned", dm(45, 50), text("psst"), nil},
		{"direct, peer in do not disturb", dm(46, 50), text("psst"), nil},
		{"direct, peer's account not the registration", dm(60, 50), text("psst"), nil},
		{"voice chat", chat{id: 9000, voice: 5, isVoice: true}, text("@alice"), []string{"alice" + voiceChat}},
		{"voice chat nobody reads", chat{id: 9001, voice: 9, isVoice: true}, text("@alice"), nil},
	} {
		h.pushFor(c.c, c.m)
		if got := pushes(); !slices.Equal(got, slices.Sorted(slices.Values(c.want))) {
			t.Errorf("%s: got %q, want %q", c.what, got, c.want)
		}
	}
}

// mentionsName is the pattern's answer.
func TestMentionsName(t *testing.T) {
	for _, name := range []string{"alice", "al", "alice.b", "Zoë", "kai", "x", "a(b)", "ſam"} {
		for _, text := range []string{
			"@alice", "@ALICE", "@al", "@alice.b", "@alice.", "(@alice)", "@zoë", "@ZOË", "@Zoë", "@Kai", "@KAI",
			"@x", "@X1", "@a(b)", "@sam", "@SAM", "@ſam", "a@alice", "@alice@host", " @al", "@", "", "@\xff", "@alic",
		} {
			if got, want := mentionsName(text, name), mentionPattern(name).MatchString(text); got != want {
				t.Errorf("%q in %q: got %v, want %v", name, text, got, want)
			}
		}
	}
}
