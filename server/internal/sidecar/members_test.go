package sidecar

import (
	"fmt"
	"maps"
	"reflect"
	"testing"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

func (f *fakeVoice) Users() map[uint32]string  { return maps.Clone(f.names) }
func (f *fakeVoice) Bio(user uint32) string    { return f.bios[user] }
func (f *fakeVoice) Pictures() map[uint32]bool { return maps.Clone(f.pictures) }

func (f *fakeVoice) Online() map[uint32]OnlineUser {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.online)
}

func (f *fakeVoice) setOnline(online map[uint32]OnlineUser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.online = online
}

// membersHub has registered users alice (10, with a picture), bob (20)
// and a guest account (30).
func membersHub(t *testing.T) *Hub {
	t.Helper()
	h := newHub(t, "general")
	if _, err := h.store.CreateAccount(store.Account{Name: "Guest-1", Temporary: true, CertPEM: []byte("c"), KeyPEM: []byte("k")}, clock, ""); err != nil {
		t.Fatal(err)
	}
	h.SetVoice(&fakeVoice{names: map[uint32]string{20: "bob", 10: "alice", 30: "guest-1"}, bios: map[uint32]string{20: "<b>hi</b>"}, pictures: map[uint32]bool{10: true}}, "")
	return h
}

func TestReadyListsRegisteredUsersButGuestAccounts(t *testing.T) {
	h := membersHub(t)
	c := h.Connect()
	t.Cleanup(c.Close)
	c.SetIdentity(mumble.Identity{Session: 2, Name: "someone"}) // guests see it too
	m := next(t, c)
	want := []any{
		map[string]any{"id": float64(10), "name": "alice", "picture": true},
		map[string]any{"id": float64(20), "name": "bob"},
	}
	if !reflect.DeepEqual(m["members"], want) {
		t.Fatalf("members %v", m["members"])
	}
}

func TestPictureChangesReachEveryone(t *testing.T) {
	h := membersHub(t)
	c := ready(t, h, "someone")
	h.PictureChanged(20, true)
	if m := next(t, c); m["type"] != "member" || m["id"] != float64(20) || m["picture"] != true || m["name"] != nil {
		t.Fatalf("picture %v", m)
	}
	h.PictureChanged(10, false)
	if m := next(t, c); m["type"] != "member" || m["id"] != float64(10) || m["picture"] != false {
		t.Fatalf("no picture %v", m)
	}
}

func TestUserChangesReachEveryone(t *testing.T) {
	h := membersHub(t)
	c := ready(t, h, "someone")
	h.UserChanged(40, "carol")
	if m := next(t, c); m["type"] != "member" || m["id"] != float64(40) || m["name"] != "carol" {
		t.Fatalf("new user %v", m)
	}
	h.UserChanged(30, "Guest-1") // guest accounts are not listed
	empty(t, c)
	h.UserChanged(40, "")
	if m := next(t, c); m["type"] != "member" || m["id"] != float64(40) || m["gone"] != true {
		t.Fatalf("removed user %v", m)
	}
}

func TestAGuestAccountMadePermanentIsListed(t *testing.T) {
	h := membersHub(t)
	c := ready(t, h, "someone")
	acc, err := h.store.AccountByName("Guest-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.MakePermanent(acc.ID, "hash"); err != nil {
		t.Fatal(err)
	}
	h.AccountPermanent("Guest-1")
	if m := next(t, c); m["type"] != "member" || m["id"] != float64(30) || m["name"] != "guest-1" {
		t.Fatalf("member %v", m)
	}
	h.AccountPermanent("nobody") // no registration (yet): its first visit tells
	empty(t, c)
}

// expectOnline checks the next message: a member online with a session,
// or offline (0).
func expectOnline(t *testing.T, c *Conn, id, session float64) {
	t.Helper()
	m := next(t, c)
	if got, _ := m["session"].(float64); m["type"] != "online" || m["id"] != id || got != session {
		t.Fatalf("online %v, want %v in %v", m, id, session)
	}
}

// Members are online whatever channel they are in (also one the viewer
// cannot see): only their session is told, no channel.
func TestMembersOnlineAreTold(t *testing.T) {
	h := membersHub(t)
	v := h.voice.(*fakeVoice)
	other := ready(t, h, "other")
	v.setOnline(map[uint32]OnlineUser{20: {"bob", 7}, 30: {"guest-1", 8}}) // guest accounts are left out
	c := h.Connect()
	t.Cleanup(c.Close)
	c.SetIdentity(mumble.Identity{Session: 2, Name: "someone"})
	if m := next(t, c); m["type"] != "ready" || !reflect.DeepEqual(m["online"], map[string]any{"20": float64(7)}) {
		t.Fatalf("ready %v", m["online"])
	}
	expectOnline(t, other, 20, 7) // the new ready brought the others up to date
	empty(t, c)
	// Alice joins, bob leaves; moves change nothing.
	v.setOnline(map[uint32]OnlineUser{10: {"alice", 9}})
	h.UserMoved(9, noChannel, 1)
	expectOnline(t, c, 10, 9)
	expectOnline(t, c, 20, 0)
	h.UserMoved(9, 1, 2)
	empty(t, c)
	// A new registration is online before it is listed.
	v.setOnline(map[uint32]OnlineUser{10: {"alice", 9}, 40: {"carol", 5}})
	h.UserChanged(40, "carol")
	expectOnline(t, c, 40, 5)
	if m := next(t, c); m["type"] != "member" || m["id"] != float64(40) {
		t.Fatalf("member %v", m)
	}
}

func TestBioOfARegisteredUser(t *testing.T) {
	h := membersHub(t)
	c := ready(t, h, "someone")
	c.Handle([]byte(`{"type":"bio","user":20}`))
	if m := next(t, c); m["type"] != "bio" || m["user"] != float64(20) || m["text"] != "<b>hi</b>" {
		t.Fatalf("bio %v", m)
	}
}

// Staff learn which registrations are web accounts (guest accounts too):
// removing those deletes the account.
func TestAccountUsersForStaff(t *testing.T) {
	h := membersHub(t)
	if _, err := h.store.CreateAccount(store.Account{Name: "Alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, clock, ""); err != nil {
		t.Fatal(err)
	}
	other := readyAs(t, h, 2, "other")
	other.Handle([]byte(`{"type":"account-users"}`))
	if m := next(t, other); m["type"] != "error" {
		t.Fatalf("non-staff: %v", m)
	}
	for _, session := range []uint32{1, 3} { // admin; Register only
		c := readyAs(t, h, session, fmt.Sprint("staff", session))
		c.Handle([]byte(`{"type":"account-users"}`))
		if m := next(t, c); m["type"] != "account-users" || !reflect.DeepEqual(m["ids"], []any{float64(10), float64(30)}) {
			t.Fatalf("%d: %v", session, m)
		}
	}
}
