package sidecar

import (
	"fmt"
	"html"
	"log/slog"
	"slices"
	"strings"

	"kallisti.chat/server/internal/store"
)

// Text channel mirrors: each text channel has a Mumble channel where
// Mumble clients read and write it (the voice server keeps them, see
// Voice.SyncTextChannels). Mumble users get what they missed when they
// enter one.

// Mirror is a text channel's Mumble channel (0: none yet) and name.
type Mirror struct {
	Channel uint32
	Name    string
	Topic   string
	Private bool // a new mirror is private (an existing one keeps its access)
	// Its category: an index into the categories, plus one (0: none).
	Category int
}

// MirrorCategory is a category's Mumble channel (0: none yet) and name.
type MirrorCategory struct {
	Channel uint32
	Name    string
}

// noChannel is the voice server's "not on the server" (mumbled.NoChannel).
const noChannel = ^uint32(0)

// historySize: what a Mumble user gets on entering a mirror, at most.
const historySize = 20

// syncMirrors makes the mirrors match the text channels and categories,
// saves their Mumble channels and reloads the lists. A new mirror for the
// text channel private (0: none) is private. Call it with manageMu held.
func (h *Hub) syncMirrors(private int64) error {
	channels, err := h.store.Channels()
	if err != nil {
		return err
	}
	cats, err := h.store.Categories()
	if err != nil {
		return err
	}
	wantCats := make([]MirrorCategory, len(cats))
	index := map[int64]int{}
	for i, c := range cats {
		wantCats[i], index[c.ID] = MirrorCategory{Channel: c.Mumble, Name: c.Name}, i+1
	}
	want := make([]Mirror, len(channels))
	for i, c := range channels {
		want[i] = Mirror{Channel: c.Mumble, Name: c.Name, Topic: c.Topic, Private: private != 0 && c.ID == private, Category: index[c.Category]}
	}
	catIDs, ids, err := h.voice.SyncTextTree(wantCats, want)
	if err != nil {
		return err
	}
	for i, c := range cats {
		if i < len(catIDs) && catIDs[i] != 0 && catIDs[i] != c.Mumble {
			if err := h.store.SetCategoryMumble(c.ID, catIDs[i]); err != nil {
				return err
			}
		}
	}
	for i, c := range channels {
		if i < len(ids) && ids[i] != 0 && ids[i] != c.Mumble {
			if err := h.store.SetMumbleChannel(c.ID, ids[i]); err != nil {
				return err
			}
		}
	}
	return h.reloadChannels()
}

// textOf finds the text channel a Mumble channel mirrors.
func (h *Hub) textOf(mumble uint32) (wireChannel, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.channels {
		if c.Mumble != 0 && c.Mumble == mumble {
			return c, true
		}
	}
	return wireChannel{}, false
}

// mirrorOf is the Mumble channel of a text channel (0: none).
func (h *Hub) mirrorOf(text int64) uint32 {
	mirror, _ := h.textMirror(text)
	return mirror
}

// textMirror is mirrorOf, with ok false when the text channel is gone: a
// deleted channel has no mirror, but it is not public either.
func (h *Hub) textMirror(text int64) (mirror uint32, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.channels {
		if c.ID == text {
			return c.Mumble, true
		}
	}
	return 0, false
}

// UserMoved hears the voice server's users change channels (see
// mumble.Config.OnUserMoved). A Mumble client (not a web client) that
// enters a channel with a stored chat (a text channel's mirror, or a voice
// channel it may read) gets what was said there, as one server text:
//   - registered users what they missed since they last left it (read
//     marks by user id, also across connections);
//   - guests, per connection, the newest messages on the first visit and
//     what they missed on later ones (Mumble clients keep their chat log
//     until they disconnect).
//
// Moves arrive a little late: a message written just then may come twice
// (live and in the history) or count as seen.
func (h *Hub) UserMoved(session, from, to uint32) {
	if from == noChannel || to == noChannel {
		h.onlineChanged()
	}
	h.visitsMu.Lock()
	defer h.visitsMu.Unlock()
	if h.visits == nil {
		h.visits = map[uint32]*visit{}
	}
	if from == noChannel { // a new connection: who it is, now (see visit)
		v := &visit{seen: map[int64]int64{}}
		v.user, v.native = h.voice.MumbleClient(session)
		h.visits[session] = v
		if v.native && v.user.Registered {
			h.sendMissedDirects(session, v.user.UserID)
		}
	}
	v := h.visits[session]
	if to == noChannel {
		delete(h.visits, session)
	}
	if v == nil || !v.native {
		return
	}
	if u, ok := h.voice.MumbleClient(session); ok {
		v.user = u // registered meanwhile
	}
	if v.in != 0 { // left the chat it was in
		last := h.lastMessage(v.in)
		v.seen[v.in] = last
		if v.user.Registered && last > 0 {
			h.setReadMark(v.user.UserID, v.in, last, false)
		}
		v.in = 0
	}
	if to == noChannel {
		return
	}
	chat, place, ok := h.chatAt(session, to)
	if !ok {
		return
	}
	v.in = chat
	since, visited := v.seen[chat]
	if v.user.Registered {
		var err error
		if since, visited, err = h.store.ReadMark(v.user.UserID, chat); err != nil {
			slog.Error("read mark", "err", err)
			return
		}
	}
	h.sendMissed(session, chat, place, since, visited)
}

// visit is what the hub knows about a connected user: who it is (asked
// when it connects: the voice server forgets a session before its leave
// arrives), the chat it is in, and per chat the last message it saw in
// this connection (for guests).
type visit struct {
	user   MumbleUser
	native bool // a Mumble client, not a web client
	in     int64
	seen   map[int64]int64
}

// chatAt is the stored chat of a Mumble channel that the user may read
// now (the move may be old): a text channel's mirror, or a voice
// channel's chat that exists. place names it in the heading.
func (h *Hub) chatAt(session, channel uint32) (chat int64, place string, ok bool) {
	if channel == noChannel {
		return 0, "", false
	}
	if c, ok := h.textOf(channel); ok {
		return c.ID, "#" + c.Name, h.voice.Sees(session, channel)
	}
	if !h.voice.MayRead(session, channel) {
		return 0, "", false
	}
	chat, ok, err := h.store.FindVoiceChat(channel)
	if err != nil {
		slog.Error("voice channel chat", "err", err)
	}
	return chat, "this channel", ok
}

func (h *Hub) lastMessage(channel int64) int64 {
	msgs, err := h.store.History(channel, 0, 1)
	if err != nil || len(msgs) == 0 {
		return 0
	}
	return msgs[0].ID
}

// sendMissed sends a chat's messages to a Mumble user, as one text: on a
// first visit the newest historySize, else those after since (the newest
// historySize of them, with how many came before).
func (h *Hub) sendMissed(session uint32, chat int64, place string, since int64, visited bool) {
	msgs, err := h.store.History(chat, 0, historySize+1)
	if err != nil {
		slog.Error("chat history for Mumble", "err", err)
		return
	}
	if visited {
		msgs = slices.DeleteFunc(msgs, func(m store.Message) bool { return m.ID <= since })
	}
	more := len(msgs) > historySize
	if more {
		msgs = msgs[len(msgs)-historySize:]
	}
	if len(msgs) == 0 {
		return
	}
	head := "Earlier in "
	if visited {
		head = "While you were away in "
	}
	head += html.EscapeString(place)
	if more && visited {
		if n, err := h.store.CountAfter(chat, since); err == nil {
			head += fmt.Sprintf(" (the newest %d; %d earlier in the web app)", historySize, n-historySize)
		}
	}
	h.voice.SendServerText(session, "<b>"+head+":</b>"+h.historyLines(msgs))
}

// historyLines are stored messages as HTML lines for a Mumble client's
// server text: author, time and text.
func (h *Hub) historyLines(msgs []store.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		at := m.CreatedAt.UTC().Format("Jan 2 15:04")
		b.WriteString("<br><b>" + html.EscapeString(authorLabel(m)) + "</b> <i>" + at + " UTC</i>: " + strings.ReplaceAll(html.EscapeString(h.mumbleText(m)), "\n", "<br>"))
	}
	return b.String()
}
