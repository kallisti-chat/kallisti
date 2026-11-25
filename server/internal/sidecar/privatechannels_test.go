package sidecar

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"testing"

	"golang.org/x/time/rate"

	"kallisti.chat/server/internal/mumble"
)

// Mumble channels that only session 1 (staff), members and the channel's
// moderators see.
func (f *fakeVoice) Sees(session, channel uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return session == 1 || !f.hidden[channel] || f.members[session] || f.moderates[session] == channel
}

func (f *fakeVoice) Private() []PrivateChannel {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []PrivateChannel
	for _, id := range slices.Sorted(maps.Keys(f.hidden)) {
		out = append(out, PrivateChannel{Channel: id, Members: []Member{{ID: 0, Name: "SuperUser"}, {ID: 20, Name: "bob"}}})
	}
	return out
}

func (f *fakeVoice) SetPrivate(session, channel uint32, on bool, members []uint32) error {
	if session != 1 {
		return errors.New("only admins and moderators make channels private")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hidden == nil {
		f.hidden = map[uint32]bool{}
	}
	f.hidden[channel] = on
	if !on {
		delete(f.hidden, channel)
	}
	return nil
}

func (f *fakeVoice) CreatePrivateChannel(session, parent uint32, name string) (uint32, error) {
	if session != 1 {
		return 0, errors.New("only admins and moderators make private channels")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, fmt.Sprintf("%s in %d", name, parent))
	return 500, nil
}

// privateHub has the text channels general (mirror 100) and secret
// (mirror 101, private); session 1 is staff, session 2 a member (bob, user
// 20) once added, session 3 (carol, user 30) not.
func privateHub(t *testing.T) (*Hub, *fakeVoice, int64) {
	t.Helper()
	h := newHub(t, "general", "secret")
	v := &fakeVoice{hidden: map[uint32]bool{101: true}, members: map[uint32]bool{}}
	h.SetVoice(v, "")
	return h, v, h.channels[1].ID
}

func channelNames(m map[string]any) []string {
	var out []string
	for _, c := range m["channels"].([]any) {
		out = append(out, c.(map[string]any)["name"].(string))
	}
	return out
}

func TestPrivateTextChannelsAreOnlyForWhoSeesTheirMirror(t *testing.T) {
	h, _, secret := privateHub(t)
	staff := h.Connect()
	t.Cleanup(staff.Close)
	staff.SetIdentity(mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 10})
	if r := next(t, staff); !slices.Equal(channelNames(r), []string{"general", "secret"}) || len(r["private"].([]any)) != 1 {
		t.Fatalf("staff got %v", r)
	}
	carol := h.Connect()
	t.Cleanup(carol.Close)
	carol.SetIdentity(mumble.Identity{Session: 3, Name: "carol", Registered: true, UserID: 30})
	carol.limit = rate.NewLimiter(rate.Inf, 0) // many requests in a row
	r := next(t, carol)
	if !slices.Equal(channelNames(r), []string{"general"}) || len(r["private"].([]any)) != 0 {
		t.Fatalf("carol got %v", r)
	}
	// Messages there (also @everyone) reach staff only, and count for
	// nobody else.
	ch := strconv.FormatInt(secret, 10)
	staff.Handle([]byte(`{"type":"send","channel":` + ch + `,"text":"@everyone @carol the plan"}`))
	m := next(t, staff)["message"].(map[string]any)
	if m["mentionAll"] != "everyone" {
		t.Fatalf("staff got %v", m)
	}
	id := strconv.Itoa(int(m["id"].(float64)))
	empty(t, carol)
	for _, req := range []string{
		`{"type":"history","channel":` + ch + `}`,
		`{"type":"pins","channel":` + ch + `}`,
		`{"type":"send","channel":` + ch + `,"text":"hi"}`,
		`{"type":"react","message":` + id + `,"emoji":"👍","on":true}`,
		`{"type":"edit-message","message":` + id + `,"text":"mine"}`,
		`{"type":"delete-message","message":` + id + `}`,
		`{"type":"pin","message":` + id + `,"on":true}`,
	} {
		carol.Handle([]byte(req))
		if got := next(t, carol); got["type"] != "error" {
			t.Fatalf("%s: carol got %v", req, got)
		}
	}
	for _, req := range []string{
		`{"type":"typing","channel":` + ch + `}`,
		`{"type":"previews","messages":[` + id + `]}`,
		`{"type":"mark-read","channel":` + ch + `,"message":` + id + `}`,
	} {
		carol.Handle([]byte(req))
	}
	empty(t, staff)
	carol.Handle([]byte(`{"type":"search","text":"plan"}`))
	if got := next(t, carol); got["type"] != "search-results" || len(got["messages"].([]any)) != 0 {
		t.Fatalf("carol found %v", got)
	}
	carol.Handle([]byte(`{"type":"search","text":"","channel":` + ch + `}`))
	if got := next(t, carol); got["type"] != "search-results" || len(got["messages"].([]any)) != 0 {
		t.Fatalf("carol found %v", got)
	}
	if marks, _ := h.store.ReadMarks(30); len(marks) != 0 {
		t.Fatalf("carol's read marks %v", marks)
	}
	// On a reload: no summary (unread, mentions) for it.
	again := h.Connect()
	t.Cleanup(again.Close)
	again.SetIdentity(mumble.Identity{Session: 3, Name: "carol", Registered: true, UserID: 30})
	if s := next(t, again)["summary"]; s != nil && len(s.([]any)) != 0 {
		t.Fatalf("carol's summary %v", s)
	}
	// A Mumble client's text there goes to staff only too.
	h.MumbleText(mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 10}, 101, "from mumble", nil)
	next(t, staff)
	empty(t, carol)
}

// When access changes, each client gets its channel list and the private
// channels it sees again; members for staff and the channel's members.
func TestAccessChangesSendTheChannelsAgain(t *testing.T) {
	h, v, _ := privateHub(t)
	staff := joinAs(t, h, mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 10})
	bob := joinAs(t, h, mumble.Identity{Session: 2, Name: "bob", Registered: true, UserID: 20})
	// Dave and a guest see the channel (by a Mumble ACL), but are no
	// members; the guest's user id 0 is not SuperUser's.
	dave := joinAs(t, h, mumble.Identity{Session: 4, Name: "dave", Registered: true, UserID: 40})
	guest := joinAs(t, h, mumble.Identity{Session: 5, Name: "guest"})
	v.mu.Lock()
	v.members[2], v.members[4], v.members[5] = true, true, true
	v.mu.Unlock()
	h.PrivateChanged()
	if m := next(t, bob); m["type"] != "channels" || !slices.Equal(channelNames(m), []string{"general", "secret"}) {
		t.Fatalf("bob got %v", m)
	}
	m := next(t, bob)
	p := m["channels"].([]any)
	if m["type"] != "private" || len(p) != 1 || len(p[0].(map[string]any)["members"].([]any)) != 2 {
		t.Fatalf("bob got %v", m)
	}
	for _, c := range []*Conn{dave, guest} {
		next(t, c)
		m := next(t, c)
		if p := m["channels"].([]any); m["type"] != "private" || len(p) != 1 || p[0].(map[string]any)["members"] != nil {
			t.Fatalf("%v got %v", c, m)
		}
	}
	// What depends on the channels seen comes along.
	if m := next(t, bob); m["type"] != "listen-only" {
		t.Fatalf("bob got %v", m)
	}
	if m := next(t, bob); m["type"] != "read-only" {
		t.Fatalf("bob got %v", m)
	}
	next(t, staff)
	m = next(t, staff)
	if p := m["channels"].([]any); len(p) != 1 || len(p[0].(map[string]any)["members"].([]any)) != 2 {
		t.Fatalf("staff got %v", m)
	}
	// New roles too: a new moderator sees all.
	h.RolesChanged()
	if m := next(t, bob); m["type"] != "roles" {
		t.Fatalf("bob got %v", m)
	}
	if m := next(t, bob); m["type"] != "channels" {
		t.Fatalf("bob got %v", m)
	}
}

func TestStaffMakeChannelsPrivate(t *testing.T) {
	h, v, _ := privateHub(t)
	staff := joinAs(t, h, mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 10})
	carol := joinAs(t, h, mumble.Identity{Session: 3, Name: "carol", Registered: true, UserID: 30})
	carol.Handle([]byte(`{"type":"set-private","voice":100,"on":true,"users":[30]}`))
	if m := next(t, carol); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("carol got %v", m)
	}
	staff.Handle([]byte(`{"type":"set-private","voice":100,"on":true,"users":[20]}`))
	empty(t, staff) // the voice server reports it (PrivateChanged)
	if !v.Sees(1, 100) || v.Sees(3, 100) {
		t.Fatal("not private")
	}
	// A new private text channel: its new mirror is private.
	staff.Handle([]byte(`{"type":"create-channel","name":"club","private":true}`))
	if m := next(t, staff); !slices.Contains(channelNames(m), "club") {
		t.Fatalf("staff got %v", m)
	}
	v.mu.Lock()
	synced := slices.Clone(v.synced)
	v.mu.Unlock()
	if len(synced) != 3 || !synced[2].Private || synced[0].Private || synced[1].Private {
		t.Fatalf("synced %+v", synced)
	}
	for _, c := range []*Conn{staff, staff, staff, carol, carol, carol, carol} {
		next(t, c) // the private, listen-only and read-only channels; carol's list
	}
	// A new private voice channel.
	carol.Handle([]byte(`{"type":"create-private-voice","name":"Lounge"}`))
	if m := next(t, carol); m["type"] != "error" {
		t.Fatalf("carol got %v", m)
	}
	staff.Handle([]byte(`{"type":"create-private-voice","name":"Lounge"}`))
	staff.Handle([]byte(`{"type":"create-private-voice","name":"Den","voice":7}`))
	v.mu.Lock()
	created := slices.Clone(v.created)
	v.mu.Unlock()
	if !slices.Equal(created, []string{"Lounge in 0", "Den in 7"}) {
		t.Fatalf("created %v", created)
	}
}

// A new private text channel whose mirror cannot be made would be public:
// it goes again.
func TestAPrivateChannelWithoutAMirrorGoes(t *testing.T) {
	h, v, _ := privateHub(t)
	v.noMirror = map[string]bool{"club": true}
	staff := joinAs(t, h, mumble.Identity{Session: 1, Name: "mod", Registered: true, UserID: 10})
	staff.Handle([]byte(`{"type":"create-channel","name":"club","private":true}`))
	if m := next(t, staff); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("staff got %v", m)
	}
	if m := next(t, staff); slices.Contains(channelNames(m), "club") {
		t.Fatalf("staff got %v", m)
	}
}

// A chat found before its private channel was deleted reaches nobody
// after: a missing channel has no mirror, but it is not public.
func TestADeletedPrivateChannelIsNotPublic(t *testing.T) {
	h, _, secret := privateHub(t)
	id := mumble.Identity{Session: 3, Name: "carol", Registered: true, UserID: 30}
	carol := joinAs(t, h, id)
	ch, ok := h.chatOf(secret)
	if !ok {
		t.Fatal("no chat for secret")
	}
	if err := h.store.HideChannel(secret); err != nil {
		t.Fatal(err)
	}
	if err := h.reloadChannels(); err != nil {
		t.Fatal(err)
	}
	if h.may(id, ch, false) || h.may(id, ch, true) {
		t.Error("carol may use the deleted private chat")
	}
	if h.userMay(30, ch) {
		t.Error("carol's pushes may read the deleted private chat")
	}
	h.sendTo(ch, map[string]any{"type": "message"})
	empty(t, carol)
}

// Without the built-in server there are no private channels.
func TestPrivateChannelsNeedTheBuiltInServer(t *testing.T) {
	h := newHub(t, "general")
	c := ready(t, h, "alice")
	c.Handle([]byte(`{"type":"set-private","voice":100,"on":true}`))
	if m := next(t, c); m["type"] != "error" {
		t.Fatalf("got %v", m)
	}
}
