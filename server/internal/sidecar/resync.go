package sidecar

import (
	"maps"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// readyFor is the state a client starts from (ready), without the live
// parts that sendReady adds: statuses and who is online.
func (h *Hub) readyFor(id mumble.Identity) map[string]any {
	channels := h.channelsFor(id)
	ready := map[string]any{"type": "ready", "channels": channels, "categories": h.categoriesFor(id, channels), "threads": h.threadsFor(id),
		"threadArchive": h.threadArchive(), "voiceChats": h.voice != nil, "emoji": h.customEmoji(), "stickers": h.stickerList(), "uploadMax": h.UploadMax, "maxUploads": store.MaxUploads, "polls": true,
		"myEmoji": h.userEmojiFor(id), "removedEmoji": h.removedEmoji()}
	if h.Avatars {
		ready["avatars"] = true
	}
	if h.voice != nil {
		ready["roles"], ready["listenOnly"], ready["timeouts"] = h.roles().forUser(h, id), h.listenOnlyFor(id), h.timeouts()
		ready["channelTimeouts"] = h.channelTimeoutsFor(id)
		ready["private"], ready["members"] = h.privateFor(id), h.members()
		ready["guestsReadOnly"], ready["captcha"] = h.voice.GuestsReadOnly(), h.voice.Captcha()
		if on, err := h.store.InviteOnly(); err == nil {
			ready["inviteOnly"] = on
		}
		ready["logo"] = h.logoVersion()
		if h.LiveStreams != nil {
			ready["streams"] = h.liveStreams(id)
		}
		if h.Recorder != nil {
			ready["recording"] = h.recordingFor(id)
		}
	}
	if id.Registered {
		ready["reads"], ready["summary"] = h.readState(id)
		ready["follows"] = h.followsOf(id)
		ready["channelList"] = h.channelListOf(id.UserID)
		if h.voice != nil {
			ready["directs"] = h.directs(id)
		}
	}
	return ready
}

// resendReady answers a client's "resync" request (after it lost events
// to a full outbox, see send): the whole state again, as a ready. Unlike
// sendReady, nobody else is told anything.
func (c *Conn) resendReady(id mumble.Identity) {
	ready := c.hub.readyFor(id)
	c.hub.statusMu.Lock() // as in sendReady: no status or online event slips in before it
	defer c.hub.statusMu.Unlock()
	c.mu.Lock()
	st := c.status
	c.mu.Unlock()
	ready["statuses"], ready["ownStatus"] = c.hub.statuses(), view(st, c.hub.now())
	if c.hub.voice != nil {
		ready["online"] = maps.Clone(c.hub.online)
	}
	c.send(ready)
}
