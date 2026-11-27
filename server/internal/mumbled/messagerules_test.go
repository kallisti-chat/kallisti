package mumbled

import (
	"database/sql"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"kallisti.chat/server/internal/msgrules"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

func TestRegistrationTimes(t *testing.T) {
	sqlDB, _ := sql.Open("sqlite", ":memory:")
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	// A database from before: its users count as old.
	sqlDB.Exec(`CREATE TABLE mumble_users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL UNIQUE,
		cert_hash TEXT UNIQUE, password_hash TEXT NOT NULL DEFAULT '', last_channel INTEGER NOT NULL DEFAULT 0)`)
	sqlDB.Exec(`INSERT INTO mumble_users (id, name, name_key) VALUES (1, 'Old', 'old')`)
	db, err := OpenDB(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := db.UserByName("old")
	if old == nil || !old.Since.IsZero() {
		t.Fatalf("old %+v", old)
	}
	before := time.Now().Add(-time.Second)
	db.RegisterUser("New", "nnn")
	if n, _ := db.UserByHash("nnn"); n == nil || n.Since.Before(before) || n.Since.After(time.Now()) {
		t.Fatalf("new %+v", n)
	}
}

func TestMessageRulesAreForAdmins(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db}, nil)
	su := superUser(t, addr)
	alice, _ := registered(t, addr, "alice")
	if _, err := s.SetMessageRules(alice.session, msgrules.Rules{Words: []string{"x"}}); err == nil {
		t.Fatal("a user changed the rules")
	}
	if _, err := s.MessageRules(alice.session); err == nil {
		t.Fatal("a user read the rules")
	}
	if _, err := s.SetMessageRules(su.session, msgrules.Rules{Domains: []string{"no domain"}}); err == nil {
		t.Fatal("saved a bad domain")
	}
	got, err := s.SetMessageRules(su.session, msgrules.Rules{NewMinutes: 10, Words: []string{" Spam "}, Domains: []string{"https://Junk.example/"},
		Replacements: []msgrules.Replacement{{Phrase: "nice ass", With: []string{"nice behind"}}}})
	if err != nil || got.NewMinutes != 10 || !slices.Equal(got.Words, []string{"Spam"}) || !slices.Equal(got.Domains, []string{"junk.example"}) {
		t.Fatalf("%+v %v", got, err)
	}
	restarted, err := New(Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if r := restarted.rules; r.NewMinutes != 10 || !slices.Equal(r.Words, []string{"Spam"}) || len(r.Replacements) != 1 || r.Replacements[0].With[0] != "nice behind" {
		t.Fatalf("not saved: %+v", r)
	}
	// Loaded rules are compiled too.
	if restarted.compiled.Check(msgrules.Author{}, msgrules.Message{Text: "spam"}, time.Now(), time.Time{}) != msgrules.ErrWord ||
		restarted.compiled.Replace("nice ass", func(int) int { return 0 }) != "nice behind" {
		t.Fatal("loaded rules not compiled")
	}
}

// New rules apply from the next message; the old ones no longer do.
func TestMessageRulesChange(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db}, nil)
	su := superUser(t, addr)
	alice, _ := registered(t, addr, "alice")
	check := func(text string) (string, error) {
		t.Helper()
		return s.CheckMessage(alice.session, msgrules.Message{Text: text})
	}
	if _, err := s.SetMessageRules(su.session, msgrules.Rules{Words: []string{"spam"}, Domains: []string{"junk.example"},
		Replacements: []msgrules.Replacement{{Phrase: "darn", With: []string{"oh"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := check("spam"); err != msgrules.ErrWord {
		t.Fatalf("spam: %v", err)
	}
	if _, err := check("junk.example"); err != msgrules.ErrDomain {
		t.Fatalf("junk: %v", err)
	}
	if text, _ := check("darn eggs"); text != "oh eggs" {
		t.Fatalf("replaced %q", text)
	}
	if _, err := s.SetMessageRules(su.session, msgrules.Rules{Words: []string{"eggs"}, Domains: []string{"bad.example"},
		Replacements: []msgrules.Replacement{{Phrase: "heck", With: []string{"gosh"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"spam", "junk.example", "darn"} {
		if got, err := check(text); err != nil || got != text {
			t.Fatalf("old rule %q: %q %v", text, got, err)
		}
	}
	if _, err := check("eggs"); err != msgrules.ErrWord {
		t.Fatalf("eggs: %v", err)
	}
	if _, err := check("www.bad.example"); err != msgrules.ErrDomain {
		t.Fatalf("bad: %v", err)
	}
	if text, _ := check("heck"); text != "gosh" {
		t.Fatalf("replaced %q", text)
	}
}

func TestMessageRules(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	var later atomic.Int64 // the clock runs ahead by this
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db, GuestAccount: func(name string) bool { return name == "gina" }},
		func(s *Server) { s.now = func() time.Time { return time.Now().Add(time.Duration(later.Load())) } })
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	bob, _ := registered(t, addr, "bob")
	carol, _ := registered(t, addr, "carol")
	guest := dial(t, addr, "guest")
	gina := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "gina", CertHash: "gina-hash"}}, "gina"))
	if err := s.SetModerator(su.session, aliceID, true); err != nil { // staff are exempt
		t.Fatal(err)
	}
	if _, err := s.SetMessageRules(su.session, msgrules.Rules{NewMinutes: 10, Words: []string{"spam"}, Domains: []string{"junk.example"}}); err != nil {
		t.Fatal(err)
	}
	refused := func(c *testClient, text, want string) {
		t.Helper()
		c.say(0, text)
		if d := c.expectDenied(pb.PermissionDenied_Text); !strings.Contains(d.GetReason(), want) {
			t.Fatalf("%q: %q, want %q", text, d.GetReason(), want)
		}
	}
	passes := func(c *testClient, text string) {
		t.Helper()
		c.say(0, text)
		su.expect(mumble.TypeTextMessage)
	}

	// Bob and carol registered just now: new accounts; the guest and the
	// guest account are limited too. (Few texts each: Mumble's own rate.)
	refused(bob, "buy SPAM", "blocked word")
	refused(carol, "see https://junk.example", "blocked site")
	refused(bob, "see https://ok.example", "New accounts can post links after 10 minutes")
	refused(carol, `<a href="https://x.example">https://x.example</a>`, "post links")
	refused(guest, "https://ok.example", "Guests cannot post links")
	passes(guest, "hi")
	refused(guest, "hi again", "one message every 5 seconds")
	passes(gina, "hello")
	refused(gina, "hello again", "one message every 5 seconds")
	passes(alice, "spam https://junk.example") // a moderator
	passes(alice, "and again")
	if _, err := s.CheckMessage(bob.session, msgrules.Message{Upload: true}); err == nil {
		t.Fatal("a new account may upload")
	}
	if _, err := s.CheckMessage(alice.session, msgrules.Message{Upload: true, Text: "spam"}); err != nil {
		t.Fatal(err)
	}

	// Ten minutes later bob is not new; guests stay limited.
	later.Store(int64(10 * time.Minute))
	passes(bob, "see https://ok.example")
	passes(bob, "and again")
	refused(carol, `<b>junk</b>.example`, "blocked site") // the plain text counts
	refused(guest, "https://ok.example", "Guests cannot post links")
}

func TestWordReplacements(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	var mu sync.Mutex
	var heard []string
	cfg := Config{DB: db, OnChannelText: func(t ChannelText) { mu.Lock(); heard = append(heard, t.Text); mu.Unlock() }}
	s, addr := startServerAndListen(t, "127.0.0.1", cfg, func(s *Server) { s.pick = func(n int) int { return n - 1 } })
	su := superUser(t, addr)
	alice, _ := registered(t, addr, "alice")
	if _, err := s.SetMessageRules(su.session, msgrules.Rules{Replacements: []msgrules.Replacement{{Phrase: "nice ass", With: []string{"beautiful behind", "lovely posterior"}}}}); err != nil {
		t.Fatal(err)
	}
	// Mumble clients get the replaced text as HTML, the web as plain text;
	// staff too.
	for _, c := range []struct {
		from, to   *testClient
		text, want string
	}{
		{alice, su, "<b>Nice</b> a.s.s &amp; more", "Lovely posterior &amp; more"},
		{su, alice, "nice ass", "lovely posterior"},
	} {
		c.from.say(0, c.text)
		var m pb.TextMessage
		proto.Unmarshal(c.to.expect(mumble.TypeTextMessage), &m)
		if m.GetMessage() != c.want {
			t.Fatalf("%q: %q, want %q", c.text, m.GetMessage(), c.want)
		}
	}
	// The hook runs on each sender's connection after delivery: in any order.
	eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(heard) == 2 })
	if !slices.Contains(heard, "Lovely posterior & more") {
		t.Fatalf("hook got %q", heard)
	}
	// Web messages: CheckMessage gives the text to post.
	if text, err := s.CheckMessage(alice.session, msgrules.Message{Text: "**nice ass**"}); err != nil || text != "**lovely posterior**" {
		t.Fatalf("%q %v", text, err)
	}
	// Too long after the replacement: refused.
	alice.say(0, strings.Repeat("x", maxTextLength-9)+" nice ass")
	alice.expectDenied(pb.PermissionDenied_TextTooLong)
}
