package sidecar

import (
	"fmt"
	"log/slog"
	"slices"
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

// A message that pings nobody goes to the devices that have its chat on
// "All messages" (see store.PushSubscription), with the same limits.

// own: the message is the user's (by id, or by name).
func own(m store.Message, name string, user uint32) bool {
	return byUser(mumble.Identity{Name: name, Registered: true, UserID: user}, m) || names.Key(m.Author) == names.Key(name)
}

// pings: the message pings the user (by name and id).
func pings(m store.Message, direct bool, name string, user uint32) bool {
	return !own(m, name, user) && pinged(m, direct, name)
}

// pinged is pings without the user's own messages: it needs no user id.
func pinged(m store.Message, direct bool, name string) bool {
	return direct || m.MentionAll == "everyone" || answers(m, names.Key(name)) || mentionsName(m.Text, name)
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
func pushBody(m store.Message, direct, ping bool, where string) string {
	switch {
	case direct:
		return "Message from " + m.Author
	case ping:
		return fmt.Sprintf("Mention in %s from %s", where, m.Author)
	}
	return fmt.Sprintf("Message in %s from %s", where, m.Author)
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
	if h.Push == nil || h.PushUser == nil || h.voice == nil {
		return
	}
	// Most messages ping nobody: only those who want all of the chat's
	// are asked about them.
	mayPing := c.direct || m.MentionAll == "everyone" || m.Reply != nil || strings.Contains(m.Text, "@")
	var all, subscribed []string
	var err error
	if !c.direct {
		chats := []int64{c.id}
		if c.parent != 0 {
			chats = append(chats, c.parent)
		}
		if all, err = h.store.PushNamesForAll(h.now(), chats...); err != nil {
			slog.Error("push subscriptions", "err", err)
		}
	}
	if subscribed = all; mayPing {
		if subscribed, err = h.store.PushNames(h.now()); err != nil {
			slog.Error("push subscriptions", "err", err)
		}
	}
	if len(subscribed) == 0 {
		return
	}
	online, where := h.voice.Online(), h.where(c)
	note := push.Note{Channel: c.id, Parent: c.parent, Author: m.Author, URL: fmt.Sprintf("/channels/%d/%d", c.id, m.ID)}
	for _, name := range subscribed {
		// What needs no lookup first: most names are neither pinged nor
		// want all of the chat's messages.
		ping := mayPing && pinged(m, c.direct, name)
		if !ping && !slices.Contains(all, name) {
			continue
		}
		user, ok := h.PushUser(name)
		if !ok || own(m, name, user) {
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
			// Asked again when it is sent: access may end while queued, or
			// the name be another account's.
			n := note
			n.All, n.Body = !ping, pushBody(m, c.direct, ping, where)
			n.Allowed = func() bool {
				u, ok := h.PushUser(name)
				return ok && u == user && !h.voice.UserBanned(user) && h.userMay(user, c)
			}
			h.Push(name, n)
		}
	}
}
