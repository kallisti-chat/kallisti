package sidecar

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
	"kallisti.chat/server/internal/store"
)

// joinAs is ready like readyAs, with registration.
func joinAs(t *testing.T, h *Hub, id mumble.Identity) *Conn {
	t.Helper()
	c := h.Connect()
	t.Cleanup(c.Close)
	c.SetIdentity(id)
	if m := next(t, c); m["type"] != "ready" {
		t.Fatalf("got %v", m)
	}
	return c
}

// say posts to the first text channel and returns the message id.
func say(t *testing.T, h *Hub, c *Conn, text string) int64 {
	t.Helper()
	ch := strconv.FormatInt(h.channels[0].ID, 10)
	c.Handle([]byte(`{"type":"send","channel":` + ch + `,"text":"` + text + `"}`))
	m := next(t, c)
	if m["type"] != "message" {
		t.Fatalf("send: %v", m)
	}
	return int64(m["message"].(map[string]any)["id"].(float64))
}

func TestEditAndDeleteOwnMessages(t *testing.T) {
	h := newHub(t, "general")
	alice := joinAs(t, h, mumble.Identity{Session: 1, Name: "alice", Registered: true, UserID: 435})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	id := strconv.FormatInt(say(t, h, alice, "helo"), 10)
	next(t, bob)
	alice.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"hello"}`))
	for _, c := range []*Conn{alice, bob} {
		m := next(t, c)
		msg := m["message"].(map[string]any)
		if m["type"] != "message-edited" || msg["text"] != "hello" || msg["edited"] == nil {
			t.Fatalf("got %v", m)
		}
	}
	// Bob may not touch it.
	bob.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"pwned"}`))
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("bob edited: %v", m)
	}
	bob.Handle([]byte(`{"type":"delete-message","message":` + id + `}`))
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("bob deleted: %v", m)
	}
	alice.Handle([]byte(`{"type":"delete-message","message":` + id + `}`))
	for _, c := range []*Conn{alice, bob} {
		if m := next(t, c); m["type"] != "message-deleted" || m["id"] != float64(must(strconv.Atoi(id))) {
			t.Fatalf("got %v", m)
		}
	}
}

// As in Discord: an edit can take files off (not add them); a message
// keeps text or a file.
func TestEditRemovesFiles(t *testing.T) {
	h := newHub(t, "general")
	var removed []string
	h.OnUploadDeleted = func(key string) { removed = append(removed, key) }
	alice := joinAs(t, h, mumble.Identity{Session: 1, Name: "alice", Registered: true, UserID: 435})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	for _, k := range []string{"k0", "k1"} {
		h.store.AddUpload(store.Upload{Key: k, Name: k + ".png", Type: "image/png", Size: 1, Author: "alice"}, clock)
	}
	alice.Handle([]byte(`{"type":"send","channel":1,"text":"","uploads":[{"key":"k0"},{"key":"k1"}]}`))
	id := fmt.Sprint(next(t, alice)["message"].(map[string]any)["id"])
	next(t, bob)
	for _, req := range []string{
		`{"type":"edit-message","message":` + id + `,"text":"","remove":["nope"]}`,    // not its file
		`{"type":"edit-message","message":` + id + `,"text":"","remove":["k0","k1"]}`, // nothing left
	} {
		if alice.Handle([]byte(req)); next(t, alice)["type"] != "error" {
			t.Fatalf("%s: accepted", req)
		}
	}
	bob.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"","remove":["k0"]}`))
	if next(t, bob)["type"] != "error" {
		t.Fatal("bob removed a file")
	}
	alice.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"","remove":["k0"]}`))
	for _, c := range []*Conn{alice, bob} {
		m := next(t, c)
		ups, _ := m["message"].(map[string]any)["uploads"].([]any)
		if m["type"] != "message-edited" || len(ups) != 1 || ups[0].(map[string]any)["name"] != "k1.png" {
			t.Fatalf("got %v", m)
		}
	}
	if fmt.Sprint(removed) != "[k0]" {
		t.Fatalf("files deleted: %v", removed)
	}
}

func must(n int, _ error) int { return n }

func TestGuestsEditOnlyWhatTheyWroteThisVisit(t *testing.T) {
	h := newHub(t, "general")
	first := joinAs(t, h, mumble.Identity{Session: 3, Name: "guest"})
	id := strconv.FormatInt(say(t, h, first, "mine"), 10)
	first.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"still mine"}`))
	if m := next(t, first); m["type"] != "message-edited" {
		t.Fatalf("own edit: %v", m)
	}
	// Another guest with that name later (names of guests are free).
	first.Close()
	later := joinAs(t, h, mumble.Identity{Session: 4, Name: "guest"})
	later.Handle([]byte(`{"type":"delete-message","message":` + id + `}`))
	if m := next(t, later); m["type"] != "error" {
		t.Fatalf("other guest deleted: %v", m)
	}
}

// sniffed is the identity a sniffer finds in these Murmur messages.
func sniffed(t *testing.T, msgs ...proto.Message) mumble.Identity {
	t.Helper()
	var s mumble.Sniffer
	var b []byte
	for _, m := range msgs {
		typ := uint16(mumble.TypeUserState)
		if _, ok := m.(*pb.ServerSync); ok {
			typ = mumble.TypeServerSync
		}
		body, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		b = binary.BigEndian.AppendUint16(b, typ)
		b = binary.BigEndian.AppendUint32(b, uint32(len(body)))
		b = append(b, body...)
	}
	id, _, err := s.Feed(b)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestUsersUnregisteredOnlineAreGuests(t *testing.T) {
	h := newHub(t, "general")
	// Registered at login, then unregistered by an admin.
	unregistered := func(session, userID uint32, name string) *Conn {
		s := proto.Uint32(session)
		return joinAs(t, h, sniffed(t,
			&pb.UserState{Session: s, Name: proto.String(name), UserId: proto.Uint32(userID)},
			&pb.ServerSync{Session: s},
			&pb.UserState{Session: s, UserId: proto.Uint32(0xffffffff)}))
	}
	alice := unregistered(1, 435, "alice")
	bob := unregistered(2, 504, "bob")
	id := strconv.FormatInt(say(t, h, alice, "mine"), 10)
	next(t, bob)
	bob.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"pwned"}`))
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("bob edited: %v", m)
	}
	alice.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"still mine"}`))
	if m := next(t, alice); m["type"] != "message-edited" {
		t.Fatalf("own edit: %v", m)
	}
}

func TestModeratorsDeleteAnyMessage(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "") // session 1 may manage
	var removed []string
	h.OnUploadDeleted = func(key string) { removed = append(removed, key) }
	mod := joinAs(t, h, mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 372})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	h.store.AddUpload(store.Upload{Key: "spamkey", Name: "spam.png", Type: "image/png", Size: 1, Author: "bob"}, clock)
	ch := strconv.FormatInt(h.channels[0].ID, 10)
	bob.Handle([]byte(`{"type":"send","channel":` + ch + `,"text":"spam","upload":"spamkey"}`))
	sent := next(t, bob)
	id := strconv.FormatInt(int64(sent["message"].(map[string]any)["id"].(float64)), 10)
	next(t, mod)
	mod.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"x"}`))
	if m := next(t, mod); m["type"] != "error" {
		t.Fatalf("moderator edited: %v", m)
	}
	mod.Handle([]byte(`{"type":"delete-message","message":` + id + `}`))
	if m := next(t, bob); m["type"] != "message-deleted" {
		t.Fatalf("bob got %v", m)
	}
	if len(removed) != 1 || removed[0] != "spamkey" {
		t.Fatalf("files removed: %v", removed)
	}
}

func TestAReusedGuestSessionOwnsNothing(t *testing.T) {
	h := newHub(t, "general")
	first := joinAs(t, h, mumble.Identity{Session: 5, Name: "guest"})
	id := strconv.FormatInt(say(t, h, first, "mine"), 10)
	first.Close()
	// Murmur gives the session number to the next guest.
	next5 := joinAs(t, h, mumble.Identity{Session: 5, Name: "guest"})
	next5.Handle([]byte(`{"type":"delete-message","message":` + id + `}`))
	if m := next(t, next5); m["type"] != "error" {
		t.Fatalf("reused session deleted: %v", m)
	}
}

func TestEditingAVoiceChatNeedsWrite(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	// Session 2 may not write to (or read) channel 9 in the fake.
	chat, _ := h.store.VoiceChat(9)
	m, _ := h.store.AddMessage(store.NewMessage{ChannelID: chat, Author: "bob", Verified: true, Text: "x", At: clock})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	bob.Handle([]byte(`{"type":"edit-message","message":` + strconv.FormatInt(m.ID, 10) + `,"text":"y"}`))
	if got := next(t, bob); got["type"] != "error" {
		t.Fatalf("edited without write: %v", got)
	}
}

func TestReplies(t *testing.T) {
	h := newHub(t, "general", "random")
	alice := joinAs(t, h, mumble.Identity{Session: 1, Name: "alice", Registered: true, UserID: 435})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	orig := say(t, h, alice, "question?")
	next(t, bob)
	general := strconv.FormatInt(h.channels[0].ID, 10)
	bob.Handle([]byte(`{"type":"send","channel":` + general + `,"text":"answer","reply":` + strconv.FormatInt(orig, 10) + `}`))
	m := next(t, bob)
	q, ok := m["message"].(map[string]any)["reply"].(map[string]any)
	if !ok || q["id"] != float64(orig) || q["author"] != "alice" || q["text"] != "question?" {
		t.Fatalf("reply %v", m)
	}
	random := strconv.FormatInt(h.channels[1].ID, 10)
	bob.Handle([]byte(`{"type":"send","channel":` + random + `,"text":"x","reply":` + strconv.FormatInt(orig, 10) + `}`))
	next(t, alice) // the answer above
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("cross-channel reply: %v", m)
	}
	// Alice comes back later: the reply counts as a mention of her.
	c := h.Connect()
	t.Cleanup(c.Close)
	c.SetIdentity(mumble.Identity{Session: 3, Name: "alice", Registered: true, UserID: 435})
	ready := next(t, c)
	if e := ready["summary"].([]any)[0].(map[string]any); e["mentions"] != float64(1) {
		t.Fatalf("summary %v", ready["summary"])
	}
}

func TestOwnershipIsByUserID(t *testing.T) {
	h := newHub(t, "general")
	alice := joinAs(t, h, mumble.Identity{Session: 1, Name: "alice", Registered: true, UserID: 4})
	id := strconv.FormatInt(say(t, h, alice, "mine"), 10)
	alice.Close()
	// Renamed: still hers.
	renamed := joinAs(t, h, mumble.Identity{Session: 2, Name: "alicia", Registered: true, UserID: 4})
	renamed.Handle([]byte(`{"type":"edit-message","message":` + id + `,"text":"still mine"}`))
	if m := next(t, renamed); m["type"] != "message-edited" {
		t.Fatalf("renamed: %v", m)
	}
	renamed.Close()
	// Someone else with her old name (a cleaned-up guest account's name).
	other := joinAs(t, h, mumble.Identity{Session: 3, Name: "alice", Registered: true, UserID: 9})
	other.Handle([]byte(`{"type":"delete-message","message":` + id + `}`))
	if m := next(t, other); m["type"] != "error" {
		t.Fatalf("same name, other user: %v", m)
	}
}

// Messages from before ids were kept (an external Murmur): by name.
func TestOldMessagesWithoutIDAreByName(t *testing.T) {
	h := newHub(t, "general")
	m, _ := h.store.AddMessage(store.NewMessage{ChannelID: h.channels[0].ID, Author: "bob", Verified: true, AuthorID: 5, Text: "x", At: clock})
	h.store.DB().Exec(`UPDATE messages SET author_id = NULL WHERE id = ?`, m.ID)
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 7})
	bob.Handle([]byte(`{"type":"edit-message","message":` + strconv.FormatInt(m.ID, 10) + `,"text":"y"}`))
	if got := next(t, bob); got["type"] != "message-edited" {
		t.Fatalf("got %v", got)
	}
}

// Session 1 may delete anyone's messages; the voice server checks the
// real rules (mumbled's MessagesOf).
func (f *fakeVoice) MessagesOf(actor, session, user uint32) (bool, uint32, error) {
	switch u, ok := f.users[session]; {
	case actor != 1:
		return false, 0, errors.New("you may not delete this user's messages")
	case session == 0:
		return true, user, nil
	case ok && u.Registered:
		return true, u.UserID, nil
	}
	return false, session, nil
}

// drain drops what the connections got so far.
func drain(conns ...*Conn) {
	for _, c := range conns {
		for len(c.Outbox()) > 0 {
			<-c.Outbox()
		}
	}
}

func TestStaffDeleteAUsersRecentMessages(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{users: map[uint32]MumbleUser{2: {Registered: true, UserID: 504}}}, "")
	var removed []string
	h.OnUploadDeleted = func(key string) { removed = append(removed, key) }
	mod := joinAs(t, h, mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 372})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	guest := joinAs(t, h, mumble.Identity{Session: 3, Name: "guest"})
	other := joinAs(t, h, mumble.Identity{Session: 4, Name: "other"})
	text := h.channels[0].ID
	voice, _ := h.store.VoiceChat(5)
	dm, _ := h.store.DirectChat(372, 504)
	add := func(ch int64, ago time.Duration, uploads ...store.Attachment) int64 {
		m, _ := h.store.AddMessage(store.NewMessage{ChannelID: ch, Author: "bob", Verified: true, AuthorID: 504, Text: "spam", At: clock.Add(-ago), Uploads: uploads})
		return m.ID
	}
	u, _ := h.store.AddUpload(store.Upload{Key: "spamkey", Name: "spam.png", Type: "image/png", Size: 1, Author: "bob"}, clock)
	inText, inVoice := add(text, time.Minute, store.Attachment{ID: u.ID}), add(voice, 0)
	old, inDM := add(text, 2*time.Hour), add(dm, 0)
	guestSaid := say(t, h, guest, "spam")
	otherSaid := say(t, h, other, "hi")
	drain(mod, bob, guest, other)
	// Staff only, for up to a week.
	for _, c := range []struct {
		conn *Conn
		req  string
	}{
		{bob, `{"type":"delete-messages","session":3,"seconds":3600}`},
		{mod, `{"type":"delete-messages","session":3,"seconds":0}`},
		{mod, `{"type":"delete-messages","session":3,"seconds":691200}`},
	} {
		c.conn.Handle([]byte(c.req))
		if m := next(t, c.conn); m["type"] != "error" || m["about"] != "manage" {
			t.Fatalf("%s: %v", c.req, m)
		}
	}
	// A registered user by id (offline works the same): the last hour in
	// text channels and voice channel chats, one event per chat (one
	// message: the single delete's event); direct chats stay private.
	mod.Handle([]byte(`{"type":"delete-messages","user":504,"seconds":3600}`))
	got := map[string]any{}
	for range 2 {
		m := next(t, other)
		got[jsonNum(m["channel"])] = []any{m["type"], m["id"]}
	}
	want := map[string]any{
		jsonNum(float64(text)):  []any{"message-deleted", float64(inText)},
		jsonNum(float64(voice)): []any{"message-deleted", float64(inVoice)},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(removed) != 1 || removed[0] != "spamkey" {
		t.Fatalf("files removed: %v", removed)
	}
	for _, id := range []int64{old, inDM, guestSaid, otherSaid} {
		if _, err := h.store.MessageByID(id); err != nil {
			t.Fatalf("message %d was deleted", id)
		}
	}
	// A guest by session: what it wrote in this connection.
	mod.Handle([]byte(`{"type":"delete-messages","session":3,"seconds":3600}`))
	if m := next(t, other); m["type"] != "message-deleted" || m["id"] != float64(guestSaid) {
		t.Fatalf("got %v", m)
	}
}

func TestManyRemovalsComeAsOneEventPerChat(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	mod := joinAs(t, h, mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 372})
	var ids []any
	for range 3 {
		m, _ := h.store.AddMessage(store.NewMessage{ChannelID: h.channels[0].ID, Author: "bob", Verified: true, AuthorID: 504, Text: "spam", At: clock})
		ids = append(ids, float64(m.ID))
	}
	mod.Handle([]byte(`{"type":"delete-messages","user":504,"seconds":86400}`))
	if m := next(t, mod); m["type"] != "messages-deleted" || fmt.Sprint(m["ids"]) != fmt.Sprint(ids) {
		t.Fatalf("got %v", m)
	}
	empty(t, mod)
}

func TestEditRequestCorrelation(t *testing.T) {
	h := newHub(t, "general")
	alice := joinAs(t, h, mumble.Identity{Session: 1, Name: "alice", Registered: true, UserID: 435})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 504})
	id := say(t, h, alice, "before")
	next(t, bob)
	alice.Handle(fmt.Appendf(nil, `{"type":"edit-message","message":%d,"text":"after","requestId":"edit-1"}`, id))
	next(t, alice) // broadcast is not a confirmation
	if got := next(t, bob); got["type"] != "message-edited" || got["requestId"] != nil || len(bob.Outbox()) != 0 {
		t.Fatalf("edit result leaked to another reader: %v", got)
	}
	if got := next(t, alice); got["type"] != "edit-result" || got["requestId"] != "edit-1" || got["message"] != float64(id) {
		t.Fatalf("uncorrelated result: %v", got)
	}
	for _, message := range []int64{id, -1} {
		alice.Handle(fmt.Appendf(nil, `{"type":"edit-message","message":%d,"text":"","requestId":"edit-2"}`, message))
		if got := next(t, alice); got["type"] != "error" || got["requestId"] != "edit-2" || got["editMessage"] != float64(message) {
			t.Fatalf("uncorrelated error: %v", got)
		}
	}
}

func TestEditEarlyRefusalsKeepCorrelation(t *testing.T) {
	h := newHub(t, "general")
	voice := &fakeVoice{timeouts: map[uint32]time.Time{2: clock.Add(time.Minute)}}
	h.SetVoice(voice, "")
	bob := readyAs(t, h, 2, "bob")
	bob.Handle([]byte(`{"type":"edit-message","message":9,"text":"proposed","requestId":"timeout"}`))
	if got := next(t, bob); got["requestId"] != "timeout" || got["editMessage"] != 9.0 || !strings.Contains(got["message"].(string), "timed out") {
		t.Fatalf("timeout: %v", got)
	}
	// A rate refusal takes place before changeMessage too.
	bob.limit = rate.NewLimiter(0, 0)
	bob.Handle([]byte(`{"type":"edit-message","message":9,"text":"proposed","requestId":"rate"}`))
	if got := next(t, bob); got["requestId"] != "rate" || got["editMessage"] != 9.0 || got["about"] != "rate" {
		t.Fatalf("rate: %v", got)
	}
	// A later unrelated error must not inherit the edit's correlation.
	bob.Handle([]byte(`{"type":"history","channel":-1}`))
	if got := next(t, bob); got["requestId"] != nil || got["editMessage"] != nil {
		t.Fatalf("leaked edit correlation: %v", got)
	}
}
