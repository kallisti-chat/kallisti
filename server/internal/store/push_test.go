package store

import (
	"errors"
	"fmt"
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
