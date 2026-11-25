package sidecar

import (
	"slices"

	"kallisti.chat/server/internal/mumble"
)

// Private channels (built-in server): text and voice channels that only
// their members, admins and moderators see. The voice server keeps who
// may (see Voice.Sees); a private text channel is its mirror's access.
// Clients get the text channels and the private channels they see, again
// when access changes.

// PrivateChannel is a private Mumble channel (a voice channel or a text
// channel's mirror) and its members.
type PrivateChannel struct {
	Channel uint32   `json:"channel"`
	Members []Member `json:"members,omitempty"` // for staff and members only
	// Members by its category (a private one), as Members.
	Inherited []Member `json:"inherited,omitempty"`
}

// Member is a registered user in a private channel.
type Member = Speaker

// channelsFor: the text channels the user may read.
func (h *Hub) channelsFor(id mumble.Identity) []wireChannel {
	h.mu.Lock()
	all := slices.Clone(h.channels)
	h.mu.Unlock()
	return slices.DeleteFunc(all, func(c wireChannel) bool { return !h.may(id, chat{id: c.ID}, false) })
}

// privateFor: the private channels the user sees, with members for staff,
// the channel's moderators and its members (also by its category: who
// reads it, see the member list); not for others who see it (a private
// category's channel itself, or by an ACL from a Mumble client).
func (h *Hub) privateFor(id mumble.Identity) []PrivateChannel {
	out := []PrivateChannel{}
	for _, p := range h.voice.Private() {
		if !h.voice.Sees(id.Session, p.Channel) {
			continue
		}
		if !h.voice.ModeratesIn(id.Session, p.Channel) && !h.voice.InPrivate(id.Session, p.Channel) {
			p.Members, p.Inherited = nil, nil
		}
		out = append(out, p)
	}
	return out
}

// isPrivate: the Mumble channel is private.
func (h *Hub) isPrivate(channel uint32) bool {
	return channel != 0 && slices.ContainsFunc(h.voice.Private(), func(p PrivateChannel) bool { return p.Channel == channel })
}

// sendAccess sends a client what depends on the channels it sees: the
// text channels, the private, listen-only and read-only channels and
// streams.
func (c *Conn) sendAccess(id mumble.Identity) {
	h := c.hub
	c.send(h.channelsEvent(id))
	if h.voice == nil {
		return
	}
	c.send(map[string]any{"type": "private", "channels": h.privateFor(id)})
	c.send(map[string]any{"type": "listen-only", "channels": h.listenOnlyFor(id)})
	c.send(map[string]any{"type": "read-only", "channels": h.readOnlyFor(id)})
	if h.LiveStreams != nil {
		c.send(map[string]any{"type": "streams", "streams": h.liveStreams(id)})
	}
	if h.Recorder != nil {
		c.send(map[string]any{"type": "recording", "channels": h.recordingFor(id)})
	}
}

func (h *Hub) sendAccessAll() {
	h.eachReady(func(c *Conn, id mumble.Identity) { c.sendAccess(id) })
}

// PrivateChanged runs after private channels or their members changed
// (see mumbled.Config.OnPrivateChanged).
func (h *Hub) PrivateChanged() {
	if h.voice != nil {
		h.sendAccessAll()
		h.pruneInvites()
	}
}

// setPrivate makes a Mumble channel (a voice channel or a text channel's
// mirror) private with these members, or public. The voice server checks
// the right and reports the change (PrivateChanged).
func (c *Conn) setPrivate(id mumble.Identity, req request) {
	if c.hub.voice == nil {
		c.manageError("private channels need the built-in Mumble server")
	} else if err := c.hub.voice.SetPrivate(id.Session, req.Voice, req.On, req.Users); err != nil {
		c.manageError(err.Error())
	}
}

// createPrivateVoice makes a private voice channel below parent (only
// staff see it, and a channel moderator who makes one in its channel,
// until they add members).
func (c *Conn) createPrivateVoice(id mumble.Identity, parent uint32, name string) {
	if c.hub.voice == nil {
		c.manageError("private channels need the built-in Mumble server")
	} else if _, err := c.hub.voice.CreatePrivateChannel(id.Session, parent, name); err != nil {
		c.manageError(err.Error())
	}
}
