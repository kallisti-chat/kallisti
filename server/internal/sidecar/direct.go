package sidecar

import (
	"fmt"
	"html"
	"log/slog"
	"slices"
	"unicode/utf8"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Direct messages between registered users (built-in server): a stored
// chat per pair, readable and writable by the two only.

type wirePeer struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

type wireDirect struct {
	Channel int64    `json:"channel"`
	Peer    wirePeer `json:"peer"`
}

func directEvent(channel int64, peer uint32, name string) map[string]any {
	return map[string]any{"type": "dm", "channel": channel, "peer": wirePeer{peer, name}}
}

// direct answers the chat with a user (made if new).
func (c *Conn) direct(id mumble.Identity, user uint32) {
	h := c.hub
	switch {
	case h.voice == nil || !id.Registered:
		c.sendError("direct messages need an account")
		return
	case user == id.UserID:
		c.sendError("you cannot write to yourself")
		return
	}
	name := h.voice.UserNames([]uint32{user})[user]
	if name == "" {
		c.sendError("unknown user")
		return
	}
	channel, err := h.store.DirectChat(id.UserID, user)
	if err != nil {
		slog.Error("direct chat", "err", err)
		c.sendError("cannot open the conversation")
		return
	}
	c.send(directEvent(channel, user, name))
}

// announceDirect tells the two users' connections about the chat (each
// with the other user), before a message: the recipient may not know it.
func (h *Hub) announceDirect(ch chat) {
	names := h.voice.UserNames(ch.peers[:])
	for _, conn := range h.readers(ch) {
		conn.mu.Lock()
		me := conn.identity.UserID
		conn.mu.Unlock()
		other := ch.peers[0] + ch.peers[1] - me
		conn.send(directEvent(ch.id, other, names[other]))
	}
}

// MumblePrivateText stores a private message from a Mumble client to a
// registered user (mumbled.Config.OnPrivateText) and sends it to the two
// users' web connections. delivered: the recipient's Mumble client got it
// (then it is read).
func (h *Hub) MumblePrivateText(from mumble.Identity, to uint32, text string, images []Image, delivered bool) {
	if h.voice == nil || !from.Registered || from.UserID == to {
		return
	}
	if utf8.RuneCountInString(text) > MaxTextLength {
		text = string([]rune(text)[:MaxTextLength])
	}
	channel, err := h.store.DirectChat(from.UserID, to)
	if err != nil {
		slog.Error("direct chat", "err", err)
		return
	}
	ch := chat{id: channel, direct: true, peers: [2]uint32{min(from.UserID, to), max(from.UserID, to)}}
	h.announceDirect(ch)
	if n, last := h.storeMumble(from, ch, text, images); delivered && n > 0 {
		h.readIfCaughtUp(to, ch.id, n, last)
	}
}

// readIfCaughtUp marks the last of n new messages read for a user who saw
// them on a Mumble client, unless older ones are unread (the next
// catch-up has those).
func (h *Hub) readIfCaughtUp(user uint32, channel int64, n int, last int64) {
	mark, _, err := h.store.ReadMark(user, channel)
	if err == nil {
		var after int
		if after, err = h.store.CountAfter(channel, mark); err == nil && after > n {
			return
		}
	}
	if err != nil {
		slog.Error("read mark", "err", err)
		return
	}
	h.setReadMark(user, channel, last, false)
}

// relayDirect sends a stored direct message to the other user's Mumble
// clients; seen there, it counts as read.
func (h *Hub) relayDirect(from mumble.Identity, ch chat, m store.Message) {
	other := ch.peers[0] + ch.peers[1] - from.UserID
	if h.voice.SendPrivateText(from.Session, from.Name, other, h.mumbleText(m)) {
		h.readIfCaughtUp(other, ch.id, 1, m.ID)
	}
}

// Messages per direct chat a joining Mumble client gets, at most.
const missedDirects = 20

// sendMissedDirects tells a registered user's joining Mumble client the
// direct messages it has not read, then marks them read.
func (h *Hub) sendMissedDirects(session, user uint32) {
	chats, err := h.store.DirectChats(user)
	if err != nil || len(chats) == 0 {
		return
	}
	marks, err := h.store.ReadMarks(user)
	if err != nil {
		slog.Error("read marks", "err", err)
		return
	}
	peers := make([]uint32, 0, len(chats))
	for _, p := range chats {
		peers = append(peers, p)
	}
	names := h.voice.UserNames(peers)
	for channel, peer := range chats {
		latest, msgs, err := h.store.MessagesAfter(channel, marks[channel], "")
		if err != nil {
			slog.Error("missed direct messages", "err", err)
			continue
		}
		msgs = slices.DeleteFunc(msgs, func(m store.Message) bool { return m.AuthorID != nil && *m.AuthorID == user })
		if len(msgs) == 0 {
			continue
		}
		slices.Reverse(msgs) // oldest first
		head := "Direct messages from " + html.EscapeString(names[peer]) + " while you were away"
		if len(msgs) > missedDirects {
			head += fmt.Sprintf(" (the newest %d; %d earlier in the web app)", missedDirects, len(msgs)-missedDirects)
			msgs = msgs[len(msgs)-missedDirects:]
		}
		h.voice.SendServerText(session, "<b>"+head+":</b>"+h.historyLines(msgs))
		h.setReadMark(user, channel, latest, false)
	}
}

// directs are a user's conversations with messages, for ready.
func (h *Hub) directs(id mumble.Identity) []wireDirect {
	out := []wireDirect{}
	chats, err := h.store.DirectChats(id.UserID)
	if err != nil {
		slog.Error("direct chats", "err", err)
		return out
	}
	peers := make([]uint32, 0, len(chats))
	for _, p := range chats {
		peers = append(peers, p)
	}
	names := h.voice.UserNames(peers)
	for channel, p := range chats {
		out = append(out, wireDirect{channel, wirePeer{p, names[p]}})
	}
	return out
}
