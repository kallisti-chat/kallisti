package sidecar

import (
	"fmt"
	"log/slog"
	"strings"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/push"
	"kallisti.chat/server/internal/store"
)

// Web Push (built-in server): a message that pings a registered user who
// is not connected goes to its browsers (see package push): a mention by
// name, a reply to it (unless no_ping), @everyone in a chat it may read,
// or a direct message. Not @here: that is for who is there. Not in do not
// disturb. Connected users get none: the open app notifies them.

// pings: the message pings the user (by name and id).
func pings(m store.Message, direct bool, name string, user uint32) bool {
	key := names.Key(name)
	if byUser(mumble.Identity{Name: name, Registered: true, UserID: user}, m) || names.Key(m.Author) == key {
		return false
	}
	return direct || m.MentionAll == "everyone" || answers(m, key) || mentionsName(m.Text, name)
}

// answers: m replies to the user (by name key), pinging. Not to a
// webhook's post, whose name may be the user's.
func answers(m store.Message, key string) bool {
	r := m.Reply
	return r != nil && !m.NoPing && !r.Webhook && r.Author != "" && names.Key(r.Author) == key
}

// pushTarget is a pinged user: connected, in do not disturb, banned, and
// whether it may read the chat.
type pushTarget struct{ online, dnd, banned, mayRead bool }

func wantsPush(t pushTarget) bool { return t.mayRead && !t.online && !t.dnd && !t.banned }

// pushBody is the push text: who, and where; never the message.
func pushBody(m store.Message, direct bool, where string) string {
	if direct {
		return "Message from " + m.Author
	}
	return fmt.Sprintf("Mention in %s from %s", where, m.Author)
}

// userMay: a registered user may read the chat, connected or not.
func (h *Hub) userMay(user uint32, c chat) bool { return h.offlineMay(&user, c) }

// offlineMay is userMay, with user nil for a guest.
func (h *Hub) offlineMay(user *uint32, c chat) bool {
	c, ok := h.parentOf(c)
	if !ok {
		return false
	}
	if c.direct {
		return user != nil && (*user == c.peers[0] || *user == c.peers[1])
	}
	sees, mayRead := h.voice.GuestSees, h.voice.GuestMayRead
	if user != nil {
		sees = func(ch uint32) bool { return h.voice.UserSees(*user, ch) }
		mayRead = func(ch uint32) bool { return h.voice.UserMayRead(*user, ch) }
	}
	if c.isVoice {
		return mayRead(c.voice)
	}
	mirror, ok := h.textMirror(c.id)
	return ok && (mirror == 0 || sees(mirror))
}

// where a chat is, for the push text.
func (h *Hub) where(c chat) string {
	if c.parent != 0 {
		t, _ := h.store.Thread(c.id)
		return h.where(chat{id: c.parent}) + " › " + t.Name
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.channels {
		if ch.ID == c.id {
			return "#" + ch.Name
		}
	}
	return "a voice channel chat"
}

// pushFor sends the pushes for a new message (Push sends them on its own
// time).
func (h *Hub) pushFor(c chat, m store.Message) {
	// Most messages ping nobody: no queries for them.
	if h.Push == nil || h.voice == nil || !c.direct && m.MentionAll != "everyone" && m.Reply == nil && !strings.Contains(m.Text, "@") {
		return
	}
	subscribed, err := h.store.PushNames(h.now())
	if err != nil {
		slog.Error("push subscriptions", "err", err)
	}
	if len(subscribed) == 0 {
		return
	}
	ids := map[string]uint32{}
	for id, name := range h.voice.Users() {
		ids[names.Key(name)] = id
	}
	online := h.voice.Online()
	note := push.Note{Channel: c.id, Parent: c.parent, Author: m.Author, Body: pushBody(m, c.direct, h.where(c)), URL: fmt.Sprintf("/channels/%d/%d", c.id, m.ID)}
	for _, name := range subscribed {
		user, ok := ids[names.Key(name)]
		if !ok || !pings(m, c.direct, name, user) {
			continue
		}
		_, on := online[user]
		t := pushTarget{online: on}
		t.banned = !on && h.voice.UserBanned(user)
		t.mayRead = !on && !t.banned && h.userMay(user, c)
		if t.mayRead {
			st, err := h.store.UserStatus(user)
			if err != nil {
				slog.Error("load a status", "err", err)
			}
			t.dnd = view(st, h.now()).Status == "dnd"
		}
		if wantsPush(t) {
			h.Push(name, note)
		}
	}
}
