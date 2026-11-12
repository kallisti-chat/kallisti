package sidecar

import (
	"slices"

	"kallisti.chat/server/internal/mumble"
)

// maxFileLinkAsks: messages per file-links request.
const maxFileLinkAsks = 100

// fileLinks sends messages again whose file links expire (see filelink), to
// this connection only, as edits (not marked edited); not the ones it may
// not read. A reply's quote picture is one of its links (see quoteImage).
func (c *Conn) fileLinks(id mumble.Identity, req request) {
	if c.hub.FileLinks == nil {
		return
	}
	asks := slices.Compact(slices.Sorted(slices.Values(req.Messages)))
	for _, mid := range asks[:min(len(asks), maxFileLinkAsks)] {
		m, err := c.hub.store.MessageByID(mid)
		if err != nil || len(m.Uploads) == 0 && (m.Reply == nil || m.Reply.Upload == nil) {
			continue
		}
		if ch, ok := c.hub.chatOf(m.ChannelID); ok && c.hub.may(id, ch, false) {
			c.send(map[string]any{"type": "message-edited", "message": c.hub.toWireFor(m, c.viewer())})
		}
	}
}

// MayReadUpload is the experimental private-room media gate for a file
// link without a signature: the readers of the file's chat, as for pushes.
// user is who asks (false: nobody logged in), asked only when a guest may
// not read. Direct chats keep their files public; a file not sent yet has
// no chat and is nobody's.
func (h *Hub) MayReadUpload(key string, user func() (uint32, bool)) bool {
	channel, err := h.store.UploadChannel(key)
	if err != nil {
		return false
	}
	c, ok := h.chatOf(channel)
	switch {
	case !ok:
		return false
	case c.direct, h.offlineMay(nil, c):
		return true
	}
	id, ok := user()
	return ok && h.offlineMay(&id, c)
}
