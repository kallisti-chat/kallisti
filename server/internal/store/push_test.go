package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestPushSubscriptions(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	alice, _ := s.CreateAccount(Account{Name: "Alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	bob, _ := s.CreateAccount(Account{Name: "bob", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	guest, _ := s.CreateAccount(Account{Name: "visitor", CertPEM: []byte("c"), KeyPEM: []byte("k"), Temporary: true}, now, "")
	for hash, acc := range map[string]int64{"a1": alice.ID, "a2": alice.ID, "b1": bob.ID, "g1": guest.ID} {
		if err := s.CreateSession(hash, acc, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	phone := PushSubscription{Endpoint: "https://push.example/phone", P256dh: "p", Auth: "a", Quiet: []int64{3}, Blocked: []string{"bob"}}
	laptop := PushSubscription{Endpoint: "https://push.example/laptop", P256dh: "p2", Auth: "a2"}
	for _, x := range []struct {
		session string
		sub     PushSubscription
	}{{"a1", phone}, {"a2", laptop}, {"g1", PushSubscription{Endpoint: "https://push.example/guest", P256dh: "p", Auth: "a"}}} {
		if err := s.SetPushSubscription(x.session, x.sub, now); err != nil {
			t.Fatal(err)
		}
	}
	endpoints := func(key string) []string {
		t.Helper()
		subs, err := s.PushSubscriptions(key, now)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, sub := range subs {
			out = append(out, sub.Endpoint)
		}
		slices.Sort(out)
		return out
	}
	if got := endpoints("alice"); !slices.Equal(got, []string{laptop.Endpoint, phone.Endpoint}) {
		t.Fatalf("two devices: %v", got)
	}
	if subs, _ := s.PushSubscriptions("alice", now); !slices.ContainsFunc(subs, func(p PushSubscription) bool {
		return p.Endpoint == phone.Endpoint && slices.Equal(p.Quiet, []int64{3}) && slices.Equal(p.Blocked, []string{"bob"}) && p.P256dh == "p" && p.Auth == "a"
	}) {
		t.Fatalf("stored as given: %+v", subs)
	}
	// Guest accounts get no pushes.
	if names, _ := s.PushNames(now); !slices.Equal(names, []string{"Alice"}) {
		t.Fatalf("names: %v", names)
	}
	// The same endpoint again (now bob on that phone) replaces it.
	phone.Quiet = nil
	if err := s.SetPushSubscription("b1", phone, now); err != nil {
		t.Fatal(err)
	}
	if got := endpoints("alice"); !slices.Equal(got, []string{laptop.Endpoint}) {
		t.Fatalf("moved away: %v", got)
	}
	if got := endpoints("bob"); !slices.Equal(got, []string{phone.Endpoint}) {
		t.Fatalf("moved to bob: %v", got)
	}
	// Only the owner deletes a subscription.
	if err := s.DeletePushSubscription(alice.ID, phone.Endpoint); err != nil {
		t.Fatal(err)
	}
	if got := endpoints("bob"); len(got) != 1 {
		t.Fatalf("someone else's: %v", got)
	}
	if err := s.DeletePushSubscription(bob.ID, phone.Endpoint); err != nil {
		t.Fatal(err)
	}
	if got := endpoints("bob"); len(got) != 0 {
		t.Fatalf("deleted: %v", got)
	}
	// A gone endpoint goes; so do subscriptions of ended sessions.
	if err := s.DropPushEndpoint(laptop.Endpoint); err != nil {
		t.Fatal(err)
	}
	s.SetPushSubscription("a1", phone, now)
	if err := s.DeleteSession("a1"); err != nil {
		t.Fatal(err)
	}
	if got := endpoints("alice"); len(got) != 0 {
		t.Fatalf("after logout: %v", got)
	}
	// Expired sessions push nothing, before the cleanup removes them.
	s.SetPushSubscription("a2", laptop, now)
	if got, _ := s.PushSubscriptions("alice", now.Add(2*time.Hour)); len(got) != 0 {
		t.Fatalf("expired session: %v", got)
	}
	if names, _ := s.PushNames(now.Add(2 * time.Hour)); len(names) != 0 {
		t.Fatalf("expired names: %v", names)
	}
	if err := s.SetPushSubscription("nope", laptop, now); err == nil {
		t.Fatal("unknown session")
	}
}

func TestPushSubscriptionsPerAccountAreLimited(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	a, _ := s.CreateAccount(Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	s.CreateSession("a1", a.ID, now.Add(time.Hour))
	for i := range maxPushSubscriptions + 2 {
		sub := PushSubscription{Endpoint: "https://push.example/" + string(rune('a'+i)), P256dh: "p", Auth: "a"}
		if err := s.SetPushSubscription("a1", sub, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	subs, _ := s.PushSubscriptions("alice", now)
	if len(subs) != maxPushSubscriptions || slices.ContainsFunc(subs, func(p PushSubscription) bool { return p.Endpoint == "https://push.example/a" }) {
		t.Fatalf("the oldest go: %+v", subs)
	}
}

func TestVAPIDKeysAreMadeOnce(t *testing.T) {
	s := open(t)
	calls := 0
	gen := func() (string, string, error) { calls++; return "pub", "priv", nil }
	for range 2 {
		pub, priv, err := s.VAPIDKeys(gen)
		if err != nil || pub != "pub" || priv != "priv" {
			t.Fatalf("got %q %q %v", pub, priv, err)
		}
	}
	if calls != 1 {
		t.Fatalf("generated %d times", calls)
	}
	if _, _, err := open(t).VAPIDKeys(func() (string, string, error) { return "", "", errors.New("no") }); err == nil {
		t.Fatal("generator error")
	}
}

// Chats a device set to "All messages": who has a device that wants the
// pushes of a chat (or of a thread's channel).
func TestPushNamesForAllMessages(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"alice", "bob", "carol"} {
		acc, _ := s.CreateAccount(Account{Name: name, PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
		session := fmt.Sprint("s", i)
		s.CreateSession(session, acc.ID, now.Add(time.Hour))
		all := [][]int64{{3, 5}, {5}, nil}[i]
		if err := s.SetPushSubscription(session, PushSubscription{Endpoint: "https://push.example/" + name, P256dh: "p", Auth: "a", All: all}, now); err != nil {
			t.Fatal(err)
		}
	}
	if subs, _ := s.PushSubscriptions("alice", now); len(subs) != 1 || !slices.Equal(subs[0].All, []int64{3, 5}) {
		t.Fatalf("stored as given: %+v", subs)
	}
	for _, c := range []struct {
		chats []int64
		want  []string
	}{
		{[]int64{3}, []string{"alice"}},
		{[]int64{5}, []string{"alice", "bob"}},
		{[]int64{9, 3}, []string{"alice"}}, // a thread (9) of channel 3
		{[]int64{4}, nil},
	} {
		if got, err := s.PushNamesForAll(now, c.chats...); err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%v: got %v, %v", c.chats, got, err)
		}
	}
	if got, _ := s.PushNamesForAll(now.Add(2*time.Hour), 5); len(got) != 0 {
		t.Errorf("ended sessions: %v", got)
	}
}

// pushNamesForAllScan is PushNamesForAll as it was before the index: every
// subscription is looked at.
func pushNamesForAllScan(s *Store, now time.Time, chats ...int64) ([]string, error) {
	in, args := inList(chats)
	return column[string](s.db, `SELECT DISTINCT a.name`+pushOf+` AND EXISTS (
		SELECT 1 FROM json_each(p.prefs, '$.all') WHERE value IN (`+in+`)) ORDER BY a.name`, append([]any{now.UnixMilli()}, args...)...)
}

// The indexed lookup finds the same accounts as looking at every
// subscription, for each shape of preferences a row can have.
func TestPushNamesForAllMatchesAScan(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	var accs []Account
	for _, name := range []string{"alice", "bob", "carol", "dave", "erin", "frank", "gina", "hal", "ivy", "visitor"} {
		acc, err := s.CreateAccount(Account{Name: name, PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k"), Temporary: name == "visitor"}, now, "")
		if err != nil {
			t.Fatal(err)
		}
		accs = append(accs, acc)
	}
	session := func(hash string, acc Account, expires time.Time) {
		t.Helper()
		if err := s.CreateSession(hash, acc.ID, expires); err != nil {
			t.Fatal(err)
		}
	}
	// Set through the store, as the server does.
	set := func(hash string, i int, expires time.Time, sub PushSubscription) {
		t.Helper()
		session(hash, accs[i], expires)
		sub.Endpoint, sub.P256dh, sub.Auth = "https://push.example/"+hash, "p", "a"
		if err := s.SetPushSubscription(hash, sub, now); err != nil {
			t.Fatal(err)
		}
	}
	hour := now.Add(time.Hour)
	set("alice1", 0, hour, PushSubscription{})                                                               // no lists at all
	set("bob1", 1, hour, PushSubscription{All: []int64{}})                                                   // an empty list
	set("carol1", 2, hour, PushSubscription{All: []int64{5}, Quiet: []int64{5}})                             // one chat
	set("dave1", 3, hour, PushSubscription{All: []int64{3, 5, 8}, Blocked: []string{"x"}, Loud: []int64{9}}) // several
	set("erin1", 4, hour, PushSubscription{All: []int64{3}})                                                 // two devices, two lists
	set("erin2", 4, hour, PushSubscription{All: []int64{8}})
	set("frank1", 5, hour, PushSubscription{Quiet: []int64{3}, Loud: []int64{5}}) // other lists only
	set("visitor1", 9, hour, PushSubscription{All: []int64{5}})                   // a guest account
	set("gina1", 6, now.Add(-time.Minute), PushSubscription{All: []int64{5}})     // a session that ended
	set("hal1", 7, now.Add(30*time.Minute), PushSubscription{All: []int64{5}})    // one that ends soon
	// Shapes the server does not write, which an older version or a hand
	// edit may have left.
	raw := func(hash string, i int, prefs string) {
		t.Helper()
		session(hash, accs[i], hour)
		if _, err := s.db.Exec(`INSERT INTO push_subscriptions (endpoint, session, p256dh, auth, prefs, created_at) VALUES (?, ?, 'p', 'a', ?, 0)`,
			"https://push.example/"+hash, hash, prefs); err != nil {
			t.Fatal(err)
		}
	}
	raw("ivy1", 8, `{}`)
	raw("ivy2", 8, `{"quiet":null,"blocked":null}`)
	raw("ivy3", 8, `{"all":null}`)
	raw("ivy4", 8, `{"all":[]}`)
	raw("ivy5", 8, `{"all":5}`)
	raw("ivy6", 8, `{"all":"5"}`)
	raw("ivy7", 8, `{"all":[5.0, "3"]}`)
	raw("alice2", 0, `[]`)
	raw("alice3", 0, `{"all":{"k":3}}`)
	raw("bob2", 1, `{"all":[null, 8]}`)

	for _, chats := range [][]int64{nil, {5}, {3}, {8}, {9, 3}, {3, 5, 8}, {4}, {5, 5}, {0}} {
		for _, at := range []time.Time{now, now.Add(45 * time.Minute), now.Add(2 * time.Hour)} {
			want, err := pushNamesForAllScan(s, at, chats...)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.PushNamesForAll(at, chats...)
			if err != nil || !slices.Equal(got, want) {
				t.Errorf("%v at +%v: got %v, %v; a scan finds %v", chats, at.Sub(now), got, err, want)
			}
		}
	}
	// The comparison is not empty.
	if got, _ := s.PushNamesForAll(now, 5); !slices.Equal(got, []string{"carol", "dave", "hal", "ivy"}) {
		t.Errorf("chat 5: %v", got)
	}
}

func TestPushSubscriptionsWithAllAreIndexed(t *testing.T) {
	if !hasIndex(t, open(t), "push_with_all") {
		t.Fatal("a new database has no push_with_all index")
	}
}

func TestPushAllIndexIsAddedToAnOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	acc, err := s.CreateAccount(Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("a1", acc.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPushSubscription("a1", PushSubscription{Endpoint: "https://push.example/a", P256dh: "p", Auth: "a", All: []int64{5}}, now); err != nil {
		t.Fatal(err)
	}
	// Simulate a database from before the index, with a row that is not
	// valid JSON: opening it must still work.
	if _, err := s.db.Exec(`DROP INDEX push_with_all`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO push_subscriptions (endpoint, session, p256dh, auth, prefs, created_at) VALUES ('https://push.example/bad', 'a1', 'p', 'a', '{bad', 0)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	for i := 0; i < 2; i++ { // the second open: nothing to add, and no error
		s, err = Open(path)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		if !hasIndex(t, s, "push_with_all") {
			t.Fatalf("reopen %d: the index was not added", i)
		}
		if got, err := s.PushNamesForAll(now, 5); err != nil || !slices.Equal(got, []string{"alice"}) {
			t.Fatalf("reopen %d: %v, %v", i, got, err)
		}
		s.Close()
	}
}

// BenchmarkPushNamesForAll is the lookup that runs for every chat message:
// many accounts, a third with a subscription, a few of them on "all
// messages" for some chats.
func BenchmarkPushNamesForAll(b *testing.B) {
	s, err := Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	const accounts = 4000
	want := 0
	for i := 0; i < accounts; i++ {
		acc, err := s.CreateAccount(Account{Name: fmt.Sprintf("user%d", i), PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
		if err != nil {
			b.Fatal(err)
		}
		if i%3 != 0 {
			continue
		}
		hash := fmt.Sprintf("s%d", i)
		if err := s.CreateSession(hash, acc.ID, now.Add(time.Hour)); err != nil {
			b.Fatal(err)
		}
		sub := PushSubscription{Endpoint: "https://push.example/" + hash, P256dh: "p", Auth: "a", Quiet: []int64{2}}
		if i%60 == 0 { // 5% of the subscriptions
			sub.All = []int64{int64(i%7 + 10), 5}
			want++
		}
		if err := s.SetPushSubscription(hash, sub, now); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		names, err := s.PushNamesForAll(now, 5, 9)
		if err != nil || len(names) != want {
			b.Fatalf("%d names, want %d: %v", len(names), want, err)
		}
	}
}
