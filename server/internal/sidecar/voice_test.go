package sidecar

import (
	"errors"
	"fmt"
	"kallisti.chat/server/internal/homes"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"kallisti.chat/server/internal/msgrules"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// fakeVoice: session 1 may use every channel; session 2 not channel 9.
type fakeVoice struct {
	mu    sync.Mutex
	homes homes.Rules
	sent  []string
	mods  []uint32
	// Admins besides user 7.
	admins []uint32
	// Text channel mirrors: the last sync, Mumble channels by name, and
	// texts to Mumble clients (channel texts by channel, server texts by
	// session).
	synced  []Mirror
	mirrors map[string]uint32
	// Categories: the last sync, Mumble channels by name, and each
	// mirror's category (its parent).
	syncedCats []MirrorCategory
	cats       map[string]uint32
	parents    map[uint32]uint32
	toChannel  map[uint32][]string
	toUser     map[uint32][]string
	// Who sessions are: users (others are guests), web (web clients), gone
	// (disconnected: the voice server forgets them before their leave event).
	users map[uint32]MumbleUser
	web   map[uint32]bool
	gone  map[uint32]bool
	// Sessions an invite made members (see InPrivate).
	added map[uint32]bool
	// The last SetListenOnly.
	listenOnly []ListenOnlyChannel
	// Registered users' names, and those on a Mumble client (private
	// texts reach them; sent: "from>to:text").
	names    map[uint32]string
	native   map[uint32]bool
	sentTo   []string
	bios     map[uint32]string
	pictures map[uint32]bool
	online   map[uint32]OnlineUser
	// Bans by Ban; time-outs by session.
	bans     []string
	timeouts map[uint32]time.Time
	// Registered users banned (UserBanned).
	banned map[uint32]bool
	// Private channels: hidden to all but session 1 and members (by
	// session); private voice channels made.
	hidden  map[uint32]bool
	members map[uint32]bool
	created []string
	// Mirrors that cannot be made (by name).
	noMirror map[string]bool
	// Guest sessions, and whether they may only read.
	guests         map[uint32]bool
	guestsReadOnly bool
	// The message rules, and the messages checked.
	rules   msgrules.Rules
	checked []msgrules.Message
	captcha string // "": off
	// Channel moderators: the Mumble channel each session moderates, the
	// list, and channel time-outs (by session and channel).
	moderates    map[uint32]uint32
	channelMods  []ChannelModerators
	chanTimeouts map[[2]uint32]time.Time
	// Called on each ModeratesIn (with mu held).
	onModeratesIn func(session uint32)
}

func (f *fakeVoice) SendPrivateText(from uint32, _ string, toUser uint32, text string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.native[toUser] {
		return false
	}
	f.sentTo = append(f.sentTo, fmt.Sprintf("%d>%d:%s", from, toUser, text))
	return true
}

func (f *fakeVoice) privateTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sentTo)
}

func (f *fakeVoice) UserNames(ids []uint32) map[uint32]string {
	out := map[uint32]string{}
	for _, id := range ids {
		if n, ok := f.names[id]; ok {
			out[id] = n
		}
	}
	return out
}

func (f *fakeVoice) MumbleClient(session uint32) (MumbleUser, bool) {
	return f.users[session], !f.web[session] && !f.gone[session]
}

func (f *fakeVoice) MayRead(session, channel uint32) bool {
	return !f.gone[session] && (session == 1 || channel != 9)
}
func (f *fakeVoice) MayWrite(session, channel uint32) bool { return session == 1 || channel != 9 }
func (f *fakeVoice) SendChannelText(from, channel uint32, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	if f.toChannel == nil {
		f.toChannel = map[uint32][]string{}
	}
	f.toChannel[channel] = append(f.toChannel[channel], text)
}

// New mirrors get Mumble channels 100, 101, ... by name, categories 200,
// 201, ...
func (f *fakeVoice) SyncTextTree(cats []MirrorCategory, want []Mirror) ([]uint32, []uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mirrors == nil {
		f.mirrors, f.cats = map[string]uint32{}, map[string]uint32{}
	}
	f.synced, f.syncedCats, f.parents = slices.Clone(want), slices.Clone(cats), map[uint32]uint32{}
	catIDs := make([]uint32, len(cats))
	for i, c := range cats {
		if f.cats[c.Name] == 0 {
			f.cats[c.Name] = 200 + uint32(len(f.cats))
		}
		catIDs[i] = f.cats[c.Name]
	}
	ids := make([]uint32, len(want))
	for i, m := range want {
		if f.noMirror[m.Name] {
			continue
		}
		if m.Channel != 0 { // kept, also when renamed
			f.mirrors[m.Name] = m.Channel
		}
		if f.mirrors[m.Name] == 0 {
			f.mirrors[m.Name] = 100 + uint32(len(f.mirrors))
			if m.Private {
				f.hidden[f.mirrors[m.Name]] = true
			}
		}
		ids[i] = f.mirrors[m.Name]
		if m.Category > 0 {
			f.parents[ids[i]] = catIDs[m.Category-1]
		}
	}
	return catIDs, ids, nil
}

func (f *fakeVoice) SendServerText(session uint32, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.toUser == nil {
		f.toUser = map[uint32][]string{}
	}
	f.toUser[session] = append(f.toUser[session], text)
}

func (f *fakeVoice) userTexts(session uint32) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.toUser[session])
}

func readyAs(t *testing.T, h *Hub, session uint32, name string) *Conn {
	t.Helper()
	c := h.Connect()
	t.Cleanup(c.Close)
	c.SetIdentity(mumble.Identity{Session: session, Name: name})
	if m := next(t, c); m["type"] != "ready" || m["voiceChats"] != true {
		t.Fatalf("got %v", m)
	}
	return c
}

// voiceChat asks for the stored chat of a voice channel.
func voiceChat(t *testing.T, c *Conn, voice int) float64 {
	t.Helper()
	c.Handle([]byte(`{"type":"voice-chat","voice":` + strconv.Itoa(voice) + `}`))
	m := next(t, c)
	if m["type"] != "voice-chat" || m["voice"] != float64(voice) {
		t.Fatalf("got %v", m)
	}
	return m["channel"].(float64)
}

func TestVoiceChannelChats(t *testing.T) {
	h := newHub(t, "general")
	v := &fakeVoice{}
	h.SetVoice(v, "https://chat.example")
	alice := readyAs(t, h, 1, "alice")
	bob := readyAs(t, h, 2, "bob")
	id := voiceChat(t, alice, 5)
	if again := voiceChat(t, bob, 5); again != id {
		t.Fatalf("two chats for one channel: %v %v", id, again)
	}
	// A message goes to the readers and to Mumble clients.
	alice.Handle([]byte(`{"type":"send","channel":` + strconv.Itoa(int(id)) + `,"text":"hello"}`))
	if m := next(t, alice); m["type"] != "message" {
		t.Fatalf("alice got %v", m)
	}
	if m := next(t, bob); m["type"] != "message" {
		t.Fatalf("bob got %v", m)
	}
	v.mu.Lock()
	if len(v.sent) != 1 || v.sent[0] != "hello" {
		t.Fatalf("to Mumble: %v", v.sent)
	}
	v.mu.Unlock()
	// Text from Mumble clients is stored and shown.
	h.MumbleText(mumble.Identity{Session: 3, Name: "native", Registered: true, UserID: 175}, 5, "from mumble", nil)
	if m := next(t, bob); m["type"] != "message" || m["message"].(map[string]any)["author"] != "native" {
		t.Fatalf("bob got %v", m)
	}
	next(t, alice)
	// History.
	bob.Handle([]byte(`{"type":"history","channel":` + strconv.Itoa(int(id)) + `}`))
	if m := next(t, bob); m["type"] != "history" || len(m["messages"].([]any)) != 2 {
		t.Fatalf("history %v", m)
	}
}

func TestClosedVoiceChatsStayClosed(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	alice := readyAs(t, h, 1, "alice")
	bob := readyAs(t, h, 2, "bob")
	bob.Handle([]byte(`{"type":"voice-chat","voice":9}`))
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("bob got %v", m)
	}
	id := voiceChat(t, alice, 9)
	// Bob cannot read it by its channel id either, nor gets its messages.
	bob.Handle([]byte(`{"type":"history","channel":` + strconv.Itoa(int(id)) + `}`))
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("bob got %v", m)
	}
	alice.Handle([]byte(`{"type":"send","channel":` + strconv.Itoa(int(id)) + `,"text":"secret"}`))
	next(t, alice)
	empty(t, bob)
	bob.Handle([]byte(`{"type":"send","channel":` + strconv.Itoa(int(id)) + `,"text":"let me in"}`))
	if m := next(t, bob); m["type"] != "error" {
		t.Fatalf("bob got %v", m)
	}
}

func TestVoiceChatsNeedTheBuiltInServer(t *testing.T) {
	h := newHub(t, "general")
	c := ready(t, h, "alice")
	c.Handle([]byte(`{"type":"voice-chat","voice":5}`))
	if m := next(t, c); m["type"] != "error" {
		t.Fatalf("got %v", m)
	}
}

func (f *fakeVoice) MayManage(session uint32) bool { return session == 1 }

func (f *fakeVoice) SetModerator(session, user uint32, on bool) error {
	return f.setRole(&f.mods, session, user, on)
}

func (f *fakeVoice) SetAdmin(session, user uint32, on bool) error {
	return f.setRole(&f.admins, session, user, on)
}

// Session 1 is an admin (user id 0).
func (f *fakeVoice) setRole(ids *[]uint32, session, user uint32, on bool) error {
	if session != 1 {
		return errors.New("only admins change roles")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	*ids = slices.DeleteFunc(*ids, func(id uint32) bool { return id == user })
	if on {
		*ids = append(*ids, user)
	}
	return nil
}

func (f *fakeVoice) Roles() (admins, moderators []uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uint32{7}, f.admins...), slices.Clone(f.mods)
}

func TestAdminsManageTextChannels(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	admin := readyAs(t, h, 1, "admin")
	bob := readyAs(t, h, 2, "bob")
	bob.Handle([]byte(`{"type":"create-channel","name":"games"}`))
	// Errors about channel changes say so (the client shows them apart).
	if m := next(t, bob); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("bob got %v", m)
	}
	admin.Handle([]byte(`{"type":"create-channel","name":"  Game Night "}`))
	for _, c := range []*Conn{admin, bob} {
		m := next(t, c)
		chans := m["channels"].([]any)
		if m["type"] != "channels" || len(chans) != 2 || chans[1].(map[string]any)["name"] != "game-night" {
			t.Fatalf("got %v", m)
		}
	}
	id := strconv.Itoa(int(next0(t, admin, h)))
	admin.Handle([]byte(`{"type":"rename-channel","channel":` + id + `,"name":"games"}`))
	if m := next(t, admin); m["type"] != "channels" {
		t.Fatalf("rename: %v", m)
	}
	next(t, bob)
	admin.Handle([]byte(`{"type":"order-channels","channels":[` + id + `]}`))
	m := next(t, admin)
	if chans := m["channels"].([]any); chans[0].(map[string]any)["name"] != "games" {
		t.Fatalf("order: %v", m)
	}
	next(t, bob)
	admin.Handle([]byte(`{"type":"delete-channel","channel":` + id + `}`))
	if m := next(t, admin); len(m["channels"].([]any)) != 1 {
		t.Fatalf("delete: %v", m)
	}
	admin.Handle([]byte(`{"type":"create-channel","name":"bad:name"}`))
	if m := next(t, admin); m["type"] != "error" {
		t.Fatalf("bad name: %v", m)
	}
}

// next0 is the id of the second text channel.
func next0(t *testing.T, _ *Conn, h *Hub) float64 {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	return float64(h.channels[1].ID)
}

func TestTextChannelNames(t *testing.T) {
	for in, want := range map[string]string{
		"General":       "general",
		" game  night ": "game-night",
		"Küche":         "küche",
	} {
		if got, ok := textChannelName(in); !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "a:b", strings.Repeat("x", 33), "<b>"} {
		if _, ok := textChannelName(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestModerators(t *testing.T) {
	h := newHub(t, "general")
	v := &fakeVoice{}
	h.SetVoice(v, "")
	admin := h.Connect()
	t.Cleanup(admin.Close)
	admin.SetIdentity(mumble.Identity{Session: 1, Name: "admin"})
	ready := next(t, admin)
	if r, ok := ready["roles"].(map[string]any); !ok || len(r["admins"].([]any)) != 1 || len(r["moderators"].([]any)) != 0 {
		t.Fatalf("ready %v", ready)
	}
	bob := readyAs(t, h, 2, "bob")
	for _, set := range []string{`"set-moderator","user":5,"moderator":true`, `"set-admin","user":6,"admin":true`} {
		bob.Handle([]byte(`{"type":` + set + `}`))
		if m := next(t, bob); m["type"] != "error" || m["about"] != "manage" {
			t.Fatalf("bob got %v", m)
		}
		admin.Handle([]byte(`{"type":` + set + `}`))
	}
	// The voice server reports the change; everyone gets the new roles.
	h.RolesChanged()
	for _, c := range []*Conn{admin, bob} {
		if m := next(t, c); m["type"] != "roles" || len(m["moderators"].([]any)) != 1 || len(m["admins"].([]any)) != 2 {
			t.Fatalf("got %v", m)
		}
	}
}

func TestMumbleImagesBecomeAttachmentsOfOneMessage(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	var saved []string
	h.SaveFile = func(author, name, ctype string, data []byte) (store.Upload, error) {
		saved = append(saved, author+":"+name+":"+ctype)
		return h.store.AddUpload(store.Upload{Key: "k" + strconv.Itoa(len(saved)), Name: name, Type: ctype, Size: int64(len(data)), Author: author}, clock)
	}
	web := readyAs(t, h, 2, "web")
	// In the #general mirror (Mumble channel 100 in the fake).
	h.MumbleText(mumble.Identity{Session: 9, Name: "Jukebox", Registered: true, UserID: 814}, 100, "Now playing: x", []Image{{Type: "image/jpeg", Data: []byte("a")}, {Type: "image/png", Data: []byte("b")}})
	// One message with all of them, in order.
	m := next(t, web)["message"].(map[string]any)
	ups, _ := m["uploads"].([]any)
	if m["text"] != "Now playing: x" || len(ups) != 2 || ups[0].(map[string]any)["type"] != "image/jpeg" || ups[1].(map[string]any)["type"] != "image/png" {
		t.Fatalf("message %v", m)
	}
	empty(t, web)
	if len(saved) != 2 || saved[0] != "Jukebox:image-1.jpg:image/jpeg" {
		t.Fatalf("saved %v", saved)
	}
}

func TestGuestsImagesAreNotStored(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	h.SaveFile = func(string, string, string, []byte) (store.Upload, error) {
		t.Fatal("stored a guest's image")
		return store.Upload{}, nil
	}
	web := readyAs(t, h, 2, "web")
	h.MumbleText(mumble.Identity{Session: 9, Name: "guest", Registered: false}, 100, "look", []Image{{Type: "image/png", Data: []byte("a")}})
	if m := next(t, web)["message"].(map[string]any); m["text"] != "look" || m["upload"] != nil {
		t.Fatalf("message %v", m)
	}
}

// Session 1 may: the fake keeps the last change.
func (f *fakeVoice) SetListenOnly(session, channel uint32, on bool, speakers []uint32) error {
	if session != 1 {
		return errors.New("only admins and moderators make channels listen-only")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listenOnly = nil
	if on {
		f.listenOnly = []ListenOnlyChannel{{Channel: channel, Speakers: []Speaker{}}}
		for _, id := range speakers {
			f.listenOnly[0].Speakers = append(f.listenOnly[0].Speakers, Speaker{ID: id, Name: "user" + strconv.Itoa(int(id))})
		}
	}
	return nil
}

func (f *fakeVoice) ListenOnly() []ListenOnlyChannel {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.listenOnly)
}

func TestListenOnlyChannels(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	mod := h.Connect()
	t.Cleanup(mod.Close)
	mod.SetIdentity(mumble.Identity{Session: 1, Name: "mod"})
	if lo, ok := next(t, mod)["listenOnly"].([]any); !ok || len(lo) != 0 {
		t.Fatal("no listen-only list in ready")
	}
	bob := readyAs(t, h, 2, "bob")
	bob.Handle([]byte(`{"type":"set-listen-only","voice":4,"on":true}`))
	if m := next(t, bob); m["type"] != "error" || m["about"] != "manage" {
		t.Fatalf("bob got %v", m)
	}
	mod.Handle([]byte(`{"type":"set-listen-only","voice":4,"on":true,"users":[9]}`))
	// The voice server reports the change; everyone gets the list.
	h.ListenOnlyChanged()
	for _, c := range []*Conn{mod, bob} {
		m := next(t, c)
		lo := m["channels"].([]any)
		if m["type"] != "listen-only" || len(lo) != 1 {
			t.Fatalf("got %v", m)
		}
		ch := lo[0].(map[string]any)
		sp := ch["speakers"].([]any)[0].(map[string]any)
		if ch["channel"] != float64(4) || sp["id"] != float64(9) || sp["name"] != "user9" {
			t.Fatalf("got %v", m)
		}
	}
}

func TestMumbleClientsGetALinkForEachFile(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "https://chat.example/")
	m := store.Message{Text: "look", Uploads: []store.Upload{{Key: "a", Name: "a.png"}, {Key: "b", Name: "b c.png"}}}
	if got, want := h.mumbleText(m), "look https://chat.example/files/a/a.png https://chat.example/files/b/b%20c.png"; got != want {
		t.Errorf("got %q", got)
	}
}
