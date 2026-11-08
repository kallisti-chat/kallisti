package sidecar

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

var clock = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func newHub(t *testing.T, channels ...string) *Hub {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.EnsureChannels(channels); err != nil {
		t.Fatal(err)
	}
	h, err := NewHub(s, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// next returns the next outgoing message of a connection, decoded.
func next(t *testing.T, c *Conn) map[string]any {
	t.Helper()
	select {
	case raw := <-c.Outbox():
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	case <-time.After(time.Second):
		t.Fatal("no message")
		return nil
	}
}

func empty(t *testing.T, c *Conn) {
	t.Helper()
	select {
	case raw := <-c.Outbox():
		t.Fatalf("unexpected message %s", raw)
	default:
	}
}

func ready(t *testing.T, h *Hub, name string) *Conn {
	t.Helper()
	c := h.Connect()
	t.Cleanup(c.Close)
	c.SetIdentity(mumble.Identity{Session: 1, Name: name})
	if m := next(t, c); m["type"] != "ready" {
		t.Fatalf("got %v", m)
	}
	return c
}

func TestReadySendsTheChannelList(t *testing.T) {
	h := newHub(t, "general", "links")
	c := h.Connect()
	defer c.Close()
	empty(t, c)
	c.SetIdentity(mumble.Identity{Session: 4, Name: "alice"})
	m := next(t, c)
	chans := m["channels"].([]any)
	if m["type"] != "ready" || len(chans) != 2 || chans[0].(map[string]any)["name"] != "general" {
		t.Fatalf("got %v", m)
	}
	// A rename sends no second ready.
	c.SetIdentity(mumble.Identity{Session: 4, Name: "alicia"})
	empty(t, c)
}

func TestRequestsBeforeIdentityAreRefused(t *testing.T) {
	h := newHub(t, "general")
	c := h.Connect()
	defer c.Close()
	c.Handle([]byte(`{"type":"send","channel":1,"text":"hi"}`))
	if m := next(t, c); m["type"] != "error" {
		t.Fatalf("got %v", m)
	}
}

func TestSendBroadcastsToEveryoneAndStores(t *testing.T) {
	h := newHub(t, "general")
	alice, bob := ready(t, h, "alice"), ready(t, h, "bob")
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"  hello  "}`))
	for _, c := range []*Conn{alice, bob} {
		m := next(t, c)
		msg := m["message"].(map[string]any)
		if m["type"] != "message" || msg["author"] != "alice" || msg["text"] != "hello" ||
			msg["channel"] != 1.0 || msg["at"] != float64(clock.UnixMilli()) {
			t.Fatalf("got %v", m)
		}
	}
	bob.Handle([]byte(`{"type":"history","channel":1}`))
	m := next(t, bob)
	msgs := m["messages"].([]any)
	if m["type"] != "history" || len(msgs) != 1 || m["more"] != false {
		t.Fatalf("got %v", m)
	}
}

func TestHistoryPages(t *testing.T) {
	h := newHub(t, "general")
	c := ready(t, h, "alice")
	for i := 0; i < pageSize+5; i++ {
		if _, err := h.store.AddMessage(store.NewMessage{ChannelID: 1, Author: "alice", Text: "x", At: clock}); err != nil {
			t.Fatal(err)
		}
	}
	c.Handle([]byte(`{"type":"history","channel":1}`))
	first := next(t, c)
	if len(first["messages"].([]any)) != pageSize || first["more"] != true {
		t.Fatalf("first page: %d more=%v", len(first["messages"].([]any)), first["more"])
	}
	oldest := first["messages"].([]any)[0].(map[string]any)["id"]
	c.Handle([]byte(`{"type":"history","channel":1,"before":` + jsonNum(oldest) + `}`))
	second := next(t, c)
	if len(second["messages"].([]any)) != 5 || second["more"] != false {
		t.Fatalf("second page: %v", second)
	}
}

func jsonNum(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestInvalidRequests(t *testing.T) {
	h := newHub(t, "general")
	c := ready(t, h, "alice")
	for _, req := range []string{
		`not json`,
		`{"type":"send","channel":99,"text":"x"}`,
		`{"type":"send","channel":1,"text":"   "}`,
		`{"type":"send","channel":1,"text":"` + strings.Repeat("a", MaxTextLength+1) + `"}`,
		`{"type":"history","channel":99}`,
		`{"type":"dance"}`,
	} {
		c.Handle([]byte(req))
		if m := next(t, c); m["type"] != "error" {
			t.Errorf("%.40s: got %v", req, m)
		}
	}
}

func TestClosedConnectionsGetNoBroadcasts(t *testing.T) {
	h := newHub(t, "general")
	alice, bob := ready(t, h, "alice"), ready(t, h, "bob")
	bob.Close()
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"hi"}`))
	next(t, alice)
	if _, ok := <-bob.Outbox(); ok {
		t.Fatal("closed connection got a message")
	}
}

func TestSlowClientsDoNotBlockOthers(t *testing.T) {
	h := newHub(t, "general")
	alice, slow := ready(t, h, "alice"), ready(t, h, "slow")
	_ = slow // never reads
	done := make(chan struct{})
	go func() {
		for i := 0; i < outboxSize*3; i++ {
			h.broadcast(map[string]any{"type": "message"})
			<-alice.Outbox()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a slow client blocked the hub")
	}
}

func TestIdentityAfterCloseDoesNotRegister(t *testing.T) {
	h := newHub(t, "general")
	c := h.Connect()
	c.Close()
	c.SetIdentity(mumble.Identity{Session: 1, Name: "late"})
	if n := h.count(); n != 0 {
		t.Fatalf("%d connections registered after close", n)
	}
}

func TestRateLimit(t *testing.T) {
	h := newHub(t, "general")
	c := ready(t, h, "alice")
	errors := 0
	for i := 0; i < burst+5; i++ {
		c.Handle([]byte(`{"type":"history","channel":1}`))
		if m := next(t, c); m["type"] == "error" {
			errors++
		}
	}
	if errors == 0 {
		t.Fatal("no request was limited")
	}
}

// Over the limit: marked, so the client can tell it from a real error; a
// send comes back with its text for the composer.
func TestRateLimitIsMarked(t *testing.T) {
	h := newHub(t, "general")
	c := ready(t, h, "alice")
	for i := 0; i < burst; i++ {
		c.Handle([]byte(`{"type":"history","channel":1}`))
		next(t, c)
	}
	c.Handle([]byte(`{"type":"history","channel":1}`))
	if m := next(t, c); m["type"] != "error" || m["about"] != "rate" {
		t.Fatalf("history: %v", m)
	}
	c.Handle([]byte(`{"type":"send","channel":1,"text":"hi"}`))
	if m := next(t, c); m["about"] != "refused" || m["text"] != "hi" || m["channel"] != 1.0 {
		t.Fatalf("send: %v", m)
	}
}

func TestUploadTokens(t *testing.T) {
	h := newHub(t, "general")
	c := ready(t, h, "alice")
	c.Handle([]byte(`{"type":"upload-token"}`))
	m := next(t, c)
	token, _ := m["token"].(string)
	if m["type"] != "upload-token" || len(token) < 32 {
		t.Fatalf("got %v", m)
	}
	if author, ok := h.RedeemUploadToken(token); !ok || author != "alice" {
		t.Fatalf("redeem: %q %v", author, ok)
	}
	if _, ok := h.RedeemUploadToken(token); ok {
		t.Fatal("token used twice")
	}
	if _, ok := h.RedeemUploadToken("made-up"); ok {
		t.Fatal("unknown token accepted")
	}
}

// Several files: their tokens in one request (count, at most
// store.MaxUploads), each working once.
func TestSeveralUploadTokens(t *testing.T) {
	h := newHub(t, "general")
	c := ready(t, h, "alice")
	c.Handle([]byte(`{"type":"upload-token","count":3}`))
	m := next(t, c)
	tokens, _ := m["tokens"].([]any)
	if m["type"] != "upload-token" || len(tokens) != 3 || m["token"] != tokens[0] {
		t.Fatalf("got %v", m)
	}
	for _, tok := range tokens {
		if author, ok := h.RedeemUploadToken(tok.(string)); !ok || author != "alice" {
			t.Fatalf("redeem %v: %q %v", tok, author, ok)
		}
	}
	c.Handle([]byte(`{"type":"upload-token","count":1000}`))
	if tokens, _ := next(t, c)["tokens"].([]any); len(tokens) != store.MaxUploads {
		t.Fatalf("count not capped: %d", len(tokens))
	}
	c.Handle([]byte(`{"type":"upload-token","count":1}`))
	if m := next(t, c); m["tokens"] != nil || m["token"] == nil {
		t.Fatalf("one: %v", m)
	}
}

func TestUploadTokensExpire(t *testing.T) {
	h := newHub(t, "general")
	now := clock
	h.now = func() time.Time { return now }
	c := ready(t, h, "alice")
	c.Handle([]byte(`{"type":"upload-token"}`))
	token := next(t, c)["token"].(string)
	now = now.Add(tokenLifetime + time.Second)
	if _, ok := h.RedeemUploadToken(token); ok {
		t.Fatal("expired token accepted")
	}
}

func TestSendWithUpload(t *testing.T) {
	h := newHub(t, "general")
	alice, bob := ready(t, h, "alice"), ready(t, h, "bob")
	if _, err := h.store.AddUpload(store.Upload{Key: "abc", Name: "cat.png", Type: "image/png", Size: 3, Author: "alice",
		Media: store.Media{Width: 640, Height: 480, Duration: 7}}, clock); err != nil {
		t.Fatal(err)
	}
	// Someone else's upload is refused.
	bob.Handle([]byte(`{"type":"send","channel":1,"text":"","upload":"abc"}`))
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("bob: %v", m)
	}
	// An attachment needs no text.
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"","upload":"abc"}`))
	m := next(t, alice)
	msg := m["message"].(map[string]any)
	up, _ := msg["upload"].(map[string]any)
	if m["type"] != "message" || up["name"] != "cat.png" || up["type"] != "image/png" || up["url"] != "/files/abc/cat.png" ||
		up["width"] != 640.0 || up["height"] != 480.0 || up["duration"] != 7.0 {
		t.Fatalf("got %v", m)
	}
	next(t, bob)
	if _, ok := up["preview"]; ok {
		t.Fatalf("preview before it was made: %v", up)
	}
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"","upload":"missing"}`))
	if m := next(t, alice); m["type"] != "error" {
		t.Fatalf("missing upload: %v", m)
	}
	// Its preview is made: the message again, with it.
	h.store.SetUploadPreview("abc", "image/webp")
	h.UploadPreview("abc")
	for _, c := range []*Conn{alice, bob} {
		m := next(t, c)
		up, _ := m["message"].(map[string]any)["upload"].(map[string]any)
		if m["type"] != "message-edited" || up["preview"] != "/files/abc/cat.png?preview" || up["previewType"] != "image/webp" || m["message"].(map[string]any)["edited"] != nil {
			t.Fatalf("preview: %v", m)
		}
	}
	// None for a refused one, nothing sent for an upload not attached.
	h.store.SetUploadPreview("abc", store.NoPreview)
	h.UploadPreview("abc")
	if up := next(t, alice)["message"].(map[string]any)["upload"].(map[string]any); up["preview"] != nil {
		t.Fatalf("refused preview: %v", up)
	}
	next(t, bob)
	h.UploadPreview("missing")
	empty(t, alice)
}

// Up to store.MaxUploads files, in order, each with a description;
// "upload" (the first) stays for clients from before the list.
func TestSendWithSeveralUploads(t *testing.T) {
	h := newHub(t, "general")
	alice, bob := ready(t, h, "alice"), ready(t, h, "bob")
	for i := range store.MaxUploads + 1 {
		if _, err := h.store.AddUpload(store.Upload{Key: fmt.Sprint("k", i), Name: fmt.Sprint(i, ".png"), Type: "image/png", Size: 3, Author: "alice"}, clock); err != nil {
			t.Fatal(err)
		}
	}
	h.store.AddUpload(store.Upload{Key: "bobs", Name: "b.png", Type: "image/png", Size: 3, Author: "bob"}, clock)
	send := func(uploads string) map[string]any {
		alice.Handle([]byte(`{"type":"send","channel":1,"text":"","uploads":` + uploads + `}`))
		return next(t, alice)
	}
	for _, refused := range []string{
		`[{"key":"k0"},{"key":"bobs"}]`, // someone else's
		`[{"key":"k0"},{"key":"k0"}]`,   // twice
		`[{"key":"k0","description":"` + strings.Repeat("a", store.MaxDescription+1) + `"}]`,
		`[` + strings.Repeat(`{"key":"k0"},`, store.MaxUploads) + `{"key":"k1"}]`, // too many
	} {
		if m := send(refused); m["type"] != "error" {
			t.Fatalf("%s: %v", refused, m)
		}
	}
	m := send(`[{"key":"k2"},{"key":"k1","description":" a cat "}]`)
	msg := m["message"].(map[string]any)
	ups, _ := msg["uploads"].([]any)
	if m["type"] != "message" || len(ups) != 2 {
		t.Fatalf("got %v", m)
	}
	first, second := ups[0].(map[string]any), ups[1].(map[string]any)
	if first["name"] != "2.png" || first["description"] != nil || second["name"] != "1.png" || second["description"] != "a cat" {
		t.Fatalf("uploads: %v", ups)
	}
	if old, _ := msg["upload"].(map[string]any); old["name"] != "2.png" {
		t.Fatalf("upload: %v", msg["upload"])
	}
	next(t, bob)
	// The old form still works.
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"","upload":"k3"}`))
	if ups, _ := next(t, alice)["message"].(map[string]any)["uploads"].([]any); len(ups) != 1 {
		t.Fatalf("old form: %v", ups)
	}
}

func TestNoReadyWithoutAName(t *testing.T) {
	h := newHub(t, "general")
	c := h.Connect()
	defer c.Close()
	c.SetIdentity(mumble.Identity{Session: 3})
	empty(t, c)
	c.SetIdentity(mumble.Identity{Session: 3, Name: "late"})
	if m := next(t, c); m["type"] != "ready" {
		t.Fatalf("got %v", m)
	}
}

func TestVerifiedAuthor(t *testing.T) {
	h := newHub(t, "general")
	c := h.Connect()
	defer c.Close()
	c.SetIdentity(mumble.Identity{Session: 1, Name: "alice", Registered: true, UserID: 435})
	next(t, c)
	c.Handle([]byte(`{"type":"send","channel":1,"text":"hi"}`))
	msg := next(t, c)["message"].(map[string]any)
	if msg["verified"] != true {
		t.Fatalf("got %v", msg)
	}
}

func TestResyncAfterOverflow(t *testing.T) {
	h := newHub(t, "general")
	slow := ready(t, h, "slow")
	for i := 0; i < outboxSize+5; i++ {
		h.broadcast(map[string]any{"type": "message"})
	}
	// Lost messages are counted per session, for the connection stats.
	if got := h.Overflows(); got[1] != 6 || len(got) != 1 {
		t.Fatalf("overflows %v", got)
	}
	// Drain without another application event: recovery must already be queued.
	found := false
	for i := 0; i < outboxSize; i++ {
		if next(t, slow)["type"] == "resync" {
			found = true
		}
	}
	if !found {
		t.Fatal("no resync after overflow and drain without another event")
	}
	h.broadcast(map[string]any{"type": "message"})
	if m := next(t, slow); m["type"] != "message" {
		t.Fatalf("got %v after resync", m)
	}
}

func TestFail(t *testing.T) {
	h := newHub(t, "general")
	c := h.Connect()
	defer c.Close()
	c.Fail("text channels are not available")
	if m := next(t, c); m["type"] != "error" {
		t.Fatalf("got %v", m)
	}
}

func TestReadyTellsTheUploadLimit(t *testing.T) {
	h := newHub(t, "general")
	h.UploadMax = 10 << 20
	c := h.Connect()
	defer c.Close()
	c.SetIdentity(mumble.Identity{Session: 1, Name: "alice"})
	if m := next(t, c); m["uploadMax"] != float64(10<<20) || m["maxUploads"] != float64(store.MaxUploads) || m["polls"] != true {
		t.Fatalf("got %v %v %v", m["uploadMax"], m["maxUploads"], m["polls"])
	}
}

// Avatars: online users' pictures are at /api/avatars/<name> too.
func TestReadyTellsWhetherAvatarsAreServed(t *testing.T) {
	h := newHub(t, "general")
	c := h.Connect()
	defer c.Close()
	c.SetIdentity(mumble.Identity{Session: 4, Name: "alice"})
	if m := next(t, c); m["avatars"] != nil {
		t.Fatalf("got %v", m)
	}
	h.Avatars = true
	c2 := h.Connect()
	defer c2.Close()
	c2.SetIdentity(mumble.Identity{Session: 5, Name: "bob"})
	if m := next(t, c2); m["avatars"] != true {
		t.Fatalf("got %v", m)
	}
}
