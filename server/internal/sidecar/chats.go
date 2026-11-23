package sidecar

import (
	"maps"
	"slices"

	"kallisti.chat/server/internal/mumble"
)

// chat is where stored messages live: a text channel, a voice channel's
// chat, or a direct chat of two users. Access and delivery go through it.
type chat struct {
	id      int64
	voice   uint32 // with isVoice: the Mumble channel
	isVoice bool
	direct  bool
	peers   [2]uint32 // with direct: the two users' ids
	parent  int64     // a thread: its text channel (see threads.go)
}

// text: a text channel (no voice chat, direct chat or thread).
func (c chat) text() bool { return !c.isVoice && !c.direct && c.parent == 0 }

// parentOf: a thread's text channel, if it exists (else the chat as it is).
func (h *Hub) parentOf(c chat) (chat, bool) {
	if c.parent == 0 {
		return c, true
	}
	return chat{id: c.parent}, h.channelExists(c.parent)
}

// chatOf finds a chat by its store channel.
func (h *Hub) chatOf(channel int64) (chat, bool) {
	if h.channelExists(channel) {
		return chat{id: channel}, true
	}
	if h.voice != nil {
		if voice, isVoice, err := h.store.VoiceOf(channel); err != nil || isVoice {
			return chat{id: channel, voice: voice, isVoice: true}, err == nil
		}
		if a, b, direct, err := h.store.DirectPeers(channel); err != nil || direct {
			return chat{id: channel, direct: true, peers: [2]uint32{a, b}}, err == nil
		}
	}
	t, err := h.store.Thread(channel)
	if err != nil {
		return chat{}, false
	}
	return h.threadChat(t)
}

// may: the user may read (or write) the chat. Text channels are for all
// who see their mirror (private ones for members and staff, deleted ones
// for nobody); writing needs TextMessage there (no channel time-out).
// Threads are their channel's.
func (h *Hub) may(id mumble.Identity, c chat, write bool) bool {
	c, ok := h.parentOf(c)
	if !ok {
		return false
	}
	if c.direct {
		return id.Registered && (id.UserID == c.peers[0] || id.UserID == c.peers[1])
	}
	if !c.isVoice {
		mirror, ok := h.textMirror(c.id)
		return ok && (h.voice == nil || mirror == 0 || h.voice.Sees(id.Session, mirror) && (!write || h.voice.MayWrite(id.Session, mirror)))
	}
	if write {
		return h.voice.MayWrite(id.Session, c.voice)
	}
	return h.voice.MayRead(id.Session, c.voice)
}

// readers are the connections whose user may read the chat: the ready
// ones, and the agent API's event streams.
func (h *Hub) readers(c chat) []*Conn {
	var out []*Conn
	h.eachReady(func(conn *Conn, id mumble.Identity) {
		if h.may(id, c, false) {
			out = append(out, conn)
		}
	})
	h.mu.Lock()
	streams := slices.Collect(maps.Keys(h.streams))
	h.mu.Unlock()
	for _, conn := range streams {
		conn.mu.Lock()
		id := conn.identity
		conn.mu.Unlock()
		if id != nil && h.may(*id, c, false) {
			out = append(out, conn)
		}
	}
	return out
}

// eachReady calls f for each ready connection, with its user.
func (h *Hub) eachReady(f func(*Conn, mumble.Identity)) {
	for _, conn := range h.all() {
		conn.mu.Lock()
		id := conn.identity
		conn.mu.Unlock()
		if id != nil {
			f(conn, *id)
		}
	}
}

// sendTo sends to the chat's readers.
func (h *Hub) sendTo(c chat, v any) {
	for _, conn := range h.readers(c) {
		conn.send(v)
	}
}

func (h *Hub) all() []*Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := make([]*Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	return conns
}
