package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kallisti.chat/server/internal/store"
)

var clock = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

const (
	phone  = "https://fcm.googleapis.com/fcm/send/phone"
	laptop = "https://updates.push.services.mozilla.com/wpush/v2/laptop"
)

type sent struct {
	endpoint, topic string
	note            Note
}

// fakeSender records pushes and answers with a status per endpoint (else
// 201).
type fakeSender struct {
	mu     sync.Mutex
	sent   []sent
	status map[string]int
}

func (f *fakeSender) Send(_ context.Context, sub store.PushSubscription, payload []byte, topic string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n Note
	if err := json.Unmarshal(payload, &n); err != nil {
		return 0, err
	}
	f.sent = append(f.sent, sent{sub.Endpoint, topic, n})
	if s, ok := f.status[sub.Endpoint]; ok {
		return s, nil
	}
	return http.StatusCreated, nil
}

func (f *fakeSender) take() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.sent
	f.sent = nil
	return out
}

// newService: alice (sessions "a1", "a2"), and a guest account ("g1").
func newService(t *testing.T) (*Service, *fakeSender, *time.Time) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	alice, _ := st.CreateAccount(store.Account{Name: "Alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, clock, "")
	guest, _ := st.CreateAccount(store.Account{Name: "visitor", CertPEM: []byte("c"), KeyPEM: []byte("k"), Temporary: true}, clock, "")
	st.CreateSession("a1", alice.ID, clock.Add(time.Hour))
	st.CreateSession("a2", alice.ID, clock.Add(time.Hour))
	st.CreateSession("g1", guest.ID, clock.Add(time.Hour))
	now := clock
	f := &fakeSender{status: map[string]int{}}
	s := &Service{
		Store: st, Sender: f, PublicKey: "BPUB", Now: func() time.Time { return now },
		Account: func(r *http.Request) (store.Account, string, bool) {
			key := r.Header.Get("Authorization")
			acc, err := st.AccountBySession(key, now)
			return acc, key, err == nil
		},
	}
	return s, f, &now
}

func call(t *testing.T, s *Service, method, path, session, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if session != "" {
		r.Header.Set("Authorization", session)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// Keys of the right sizes: a P-256 point and 16 bytes.
var (
	p256dh = base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
	auth   = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
)

func subscribe(endpoint string, quiet ...int64) string {
	return subscribeWith(endpoint, p256dh, auth, quiet, nil)
}

func subscribeWith(endpoint, p256dh, auth string, quiet []int64, blocked []string) string {
	b, _ := json.Marshal(map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": p256dh, "auth": auth}, "quiet": quiet, "blocked": blocked})
	return string(b)
}

func TestKnownPushService(t *testing.T) {
	for url, want := range map[string]bool{
		phone:                             true,
		laptop:                            true,
		"https://web.push.apple.com/QGuQ": true,
		"https://wns2-par02p.notify.windows.com/w/?token=x": true,
		"http://fcm.googleapis.com/fcm/send/x":              false, // not HTTPS
		"https://fcm.googleapis.com.evil.example/x":         false,
		"https://evil.example/?fcm.googleapis.com":          false,
		"https://localhost/push":                            false,
		"https://user@fcm.googleapis.com/fcm/send/x":        false,
		"https://fcm.googleapis.com:8443/fcm/send/x":        false,
		"not a url": false,
		"https://xn--push-apple-com.example/push.apple.com/": false,
	} {
		if got := knownPushService(url); got != want {
			t.Errorf("%s: got %v", url, got)
		}
	}
}

func TestSubscriptionAPI(t *testing.T) {
	s, _, _ := newService(t)
	if w := call(t, s, "GET", "/api/push", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"publicKey":"BPUB"`) {
		t.Fatalf("key: %d %s", w.Code, w.Body)
	}
	for _, c := range []struct {
		session, body string
		want          int
	}{
		{"", subscribe(phone), http.StatusUnauthorized},
		{"g1", subscribe(phone), http.StatusForbidden}, // guest accounts
		{"a1", subscribe("https://evil.example/x"), http.StatusBadRequest},
		{"a1", `{"endpoint":"` + phone + `"}`, http.StatusBadRequest}, // no keys
		{"a1", subscribeWith(phone, "BKEY", auth, nil, nil), http.StatusBadRequest},
		{"a1", subscribeWith(phone, p256dh, "AUTH", nil, nil), http.StatusBadRequest},
		{"a1", "nope", http.StatusBadRequest},
		{"a1", subscribe(phone, 3), http.StatusNoContent},
		{"a2", subscribe(laptop), http.StatusNoContent},
	} {
		if w := call(t, s, "POST", "/api/push/subscriptions", c.session, c.body); w.Code != c.want {
			t.Errorf("%s %s: got %d %s", c.session, c.body, w.Code, w.Body)
		}
	}
	subs, _ := s.Store.PushSubscriptions("alice", clock)
	if len(subs) != 2 {
		t.Fatalf("two devices: %+v", subs)
	}
	// Form posts cannot reach it (CSRF), nor cross-site requests.
	r := httptest.NewRequest("POST", "/api/push/subscriptions", strings.NewReader(subscribe(phone)))
	r.Header.Set("Content-Type", "text/plain")
	r.Header.Set("Authorization", "a1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form: %d", w.Code)
	}
	r = httptest.NewRequest("DELETE", "/api/push/subscriptions", strings.NewReader(subscribe(phone)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "a1")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d", w.Code)
	}
	// Any session of the account removes a device.
	if w := call(t, s, "DELETE", "/api/push/subscriptions", "a2", `{"endpoint":"`+phone+`"}`); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if subs, _ := s.Store.PushSubscriptions("alice", clock); len(subs) != 1 || subs[0].Endpoint != laptop {
		t.Fatalf("after delete: %+v", subs)
	}
	if w := call(t, s, "DELETE", "/api/push/subscriptions", "", `{"endpoint":"`+laptop+`"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("delete without login: %d", w.Code)
	}
}

func TestDeliver(t *testing.T) {
	ctx := context.Background()
	s, f, now := newService(t)
	call(t, s, "POST", "/api/push/subscriptions", "a1", subscribe(phone, 3))
	call(t, s, "POST", "/api/push/subscriptions", "a2", subscribe(laptop))
	note := Note{Channel: 1, Body: "Mention in #general from bob", URL: "/channels/1/7"}
	s.deliver(ctx, "alice", note)
	got := f.take()
	slices.SortFunc(got, func(a, b sent) int { return strings.Compare(a.endpoint, b.endpoint) })
	if len(got) != 2 || got[0].endpoint != phone || got[0].topic != "chat-1" || got[0].note.Body != note.Body || got[0].note.URL != note.URL || got[0].note.Tag != "chat-1" {
		t.Fatalf("both devices: %+v", got)
	}
	// A device's quiet channel (set to "nothing" there) gets none.
	s.deliver(ctx, "alice", Note{Channel: 3, Body: "x", URL: "/channels/3/8"})
	if got := f.take(); len(got) != 1 || got[0].endpoint != laptop {
		t.Fatalf("quiet on the phone: %+v", got)
	}
	*now = now.Add(time.Minute)
	// Gone subscriptions are dropped; other errors keep them.
	f.status[phone], f.status[laptop] = http.StatusGone, http.StatusInternalServerError
	s.deliver(ctx, "alice", note)
	if got := f.take(); len(got) != 2 {
		t.Fatalf("after a minute: %+v", got)
	}
	subs, _ := s.Store.PushSubscriptions("alice", *now)
	if len(subs) != 1 || subs[0].Endpoint != laptop {
		t.Fatalf("410 drops: %+v", subs)
	}
	f.status[laptop] = http.StatusNotFound
	*now = now.Add(time.Minute)
	s.deliver(ctx, "Alice", note)
	if subs, _ := s.Store.PushSubscriptions("alice", *now); len(subs) != 0 {
		t.Fatalf("404 drops: %+v", subs)
	}
	s.deliver(ctx, "nobody", note) // no subscriptions: nothing
	if got := f.take(); len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}

// A thread follows its channel's "nothing" on a device, unless it has a
// setting of its own there (loud), or is set to "nothing" itself.
func TestDeliverThreadsOfQuietChannels(t *testing.T) {
	s, f, _ := newService(t)
	b, _ := json.Marshal(map[string]any{"endpoint": phone, "keys": map[string]string{"p256dh": p256dh, "auth": auth}, "quiet": []int64{3, 9}, "loud": []int64{8}})
	if w := call(t, s, "POST", "/api/push/subscriptions", "a1", string(b)); w.Code != http.StatusNoContent {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body)
	}
	for _, c := range []struct {
		note Note
		want int
	}{
		{Note{Channel: 7, Parent: 3}, 0}, // in a quiet channel
		{Note{Channel: 8, Parent: 3}, 1}, // a setting of its own
		{Note{Channel: 9, Parent: 1}, 0}, // quiet itself
		{Note{Channel: 10, Parent: 1}, 1},
	} {
		s.deliver(context.Background(), "alice", c.note)
		if got := f.take(); len(got) != c.want {
			t.Errorf("%+v: got %+v", c.note, got)
		}
	}
}

// Users blocked on a device push nothing there (the app hides them too).
func TestDeliverSkipsBlockedAuthors(t *testing.T) {
	s, f, _ := newService(t)
	call(t, s, "POST", "/api/push/subscriptions", "a1", subscribeWith(phone, p256dh, auth, nil, []string{"bob"}))
	call(t, s, "POST", "/api/push/subscriptions", "a2", subscribe(laptop))
	s.deliver(context.Background(), "alice", Note{Channel: 1, Author: "Bob", Body: "Message from Bob", URL: "/channels/1/2"})
	if got := f.take(); len(got) != 1 || got[0].endpoint != laptop {
		t.Fatalf("got %+v", got)
	}
}

func TestSendErrorsKeepTheSubscription(t *testing.T) {
	s, _, _ := newService(t)
	call(t, s, "POST", "/api/push/subscriptions", "a1", subscribe(phone))
	s.Sender = senderFunc(func(context.Context, store.PushSubscription, []byte, string) (int, error) {
		return 0, errors.New("offline")
	})
	s.deliver(context.Background(), "alice", Note{Channel: 1, Body: "x", URL: "/"})
	if subs, _ := s.Store.PushSubscriptions("alice", clock); len(subs) != 1 {
		t.Fatalf("kept: %+v", subs)
	}
}

// Notify queues at most one push per user and chat and minute (before the
// queue), and drops pushes when the queue is full.
// Before Start (or with push off) there is no queue: nothing happens.
func TestNotifyBeforeStart(t *testing.T) {
	s, f, _ := newService(t)
	s.Notify("alice", Note{Channel: 1})
	if len(f.sent) != 0 {
		t.Fatalf("sent %v", f.sent)
	}
}

func TestNotifyQueue(t *testing.T) {
	s, _, now := newService(t)
	s.Start(0, 3) // no workers: the queue fills
	defer s.Stop()
	for range 3 {
		s.Notify("alice", Note{Channel: 1})
		s.Notify("Alice", Note{Channel: 1})
	}
	s.Notify("alice", Note{Channel: 2})
	if len(s.queue) != 2 || s.dropped.Load() != 0 {
		t.Fatalf("collapsed: %d queued, %d dropped", len(s.queue), s.dropped.Load())
	}
	*now = now.Add(time.Minute)
	for _, name := range []string{"alice", "bob", "carol"} {
		s.Notify(name, Note{Channel: 1})
	}
	if len(s.queue) != 3 || s.dropped.Load() != 2 {
		t.Fatalf("full: %d queued, %d dropped", len(s.queue), s.dropped.Load())
	}
}

// Stop cancels the sends on their way and drops the queue.
func TestNotifyStop(t *testing.T) {
	s, _, _ := newService(t)
	call(t, s, "POST", "/api/push/subscriptions", "a1", subscribe(phone))
	var sends atomic.Int32
	started := make(chan struct{})
	s.Sender = senderFunc(func(ctx context.Context, _ store.PushSubscription, _ []byte, _ string) (int, error) {
		if sends.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		return 0, ctx.Err()
	})
	s.Start(1, 4)
	s.Notify("alice", Note{Channel: 1})
	s.Notify("alice", Note{Channel: 2})
	<-started
	s.Stop() // returns: the send was cancelled
	s.Notify("alice", Note{Channel: 3})
	if n := sends.Load(); n != 1 {
		t.Fatalf("%d sends", n)
	}
}

type senderFunc func(context.Context, store.PushSubscription, []byte, string) (int, error)

func (f senderFunc) Send(ctx context.Context, sub store.PushSubscription, payload []byte, topic string) (int, error) {
	return f(ctx, sub, payload, topic)
}

func TestKeys(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	if pub, priv, err := Keys(st, "p", "k"); pub != "p" || priv != "k" || err != nil {
		t.Fatalf("configured: %q %q %v", pub, priv, err)
	}
	pub, priv, err := Keys(st, "", "")
	if err != nil || len(pub) < 80 || priv == "" {
		t.Fatalf("made: %q %q %v", pub, priv, err)
	}
	if pub2, priv2, _ := Keys(st, "", ""); pub2 != pub || priv2 != priv {
		t.Fatal("kept")
	}
}

func TestWebPushSends(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	st, _ := store.Open(":memory:")
	defer st.Close()
	pub, priv, _ := Keys(st, "", "")
	browser, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	enc := base64.RawURLEncoding.EncodeToString
	sub := store.PushSubscription{Endpoint: srv.URL + "/push", P256dh: enc(browser.PublicKey().Bytes()), Auth: enc(auth)}
	status, err := NewWebPush(pub, priv, "", "https://a.example").Send(context.Background(), sub, []byte(`{"body":"x"}`), "chat-1")
	if err != nil || status != http.StatusCreated {
		t.Fatalf("got %d %v", status, err)
	}
	if got.Get("Topic") != "chat-1" || got.Get("TTL") != "43200" || got.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(got.Get("Authorization"), "vapid t=") {
		t.Fatalf("headers: %v", got)
	}
}

func TestSubject(t *testing.T) {
	for _, c := range [][3]string{
		{"mailto:admin@a.example", "https://a.example", "admin@a.example"}, // the library adds mailto:
		{"", "https://a.example", "https://a.example"},
		{"", "http://localhost:8080", "https://kallisti.chat"},
	} {
		if got := subject(c[0], c[1]); got != c[2] {
			t.Errorf("subject(%q, %q) = %q", c[0], c[1], got)
		}
	}
}

// Pushes carry the server's name and logo (none set: the service worker
// shows Kallisti's).
func TestDeliverWithTheServersNameAndLogo(t *testing.T) {
	s, f, _ := newService(t)
	call(t, s, "POST", "/api/push/subscriptions", "a1", subscribe(phone))
	s.deliver(context.Background(), "alice", Note{Channel: 1, Body: "x", URL: "/channels/1/7"})
	if got := f.take(); len(got) != 1 || got[0].note.Title != "" || got[0].note.Icon != "" {
		t.Fatalf("no brand: %+v", got)
	}
	s.Brand = func() (string, string) { return "Our place", "/api/logo/192.png?v=abc" }
	s.deliver(context.Background(), "alice", Note{Channel: 2, Body: "x", URL: "/channels/2/8"})
	if got := f.take(); len(got) != 1 || got[0].note.Title != "Our place" || got[0].note.Icon != "/api/logo/192.png?v=abc" {
		t.Fatalf("with a brand: %+v", got)
	}
}

// Another Kallisti page (several servers in one client) subscribes with
// this server's token: cross-site, with CORS, no cookies.
func TestSubscriptionsFromAnotherSite(t *testing.T) {
	s, _, _ := newService(t)
	r := httptest.NewRequest("OPTIONS", "/api/push/subscriptions", nil)
	r.Header.Set("Origin", "https://home.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://home.example" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	r = httptest.NewRequest("POST", "/api/push/subscriptions", strings.NewReader(subscribe(phone)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://home.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Authorization", "Bearer unknown")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized { // past the cross-site gate: a token, no cookie
		t.Fatalf("with a token: %d %s", w.Code, w.Body)
	}
}
