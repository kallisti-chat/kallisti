package sidecar

import (
	"errors"
	"time"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Pinned messages: moderators and admins pin and unpin (and a channel's
// moderators there), in a direct chat its two users; everyone who may
// read a chat sees its pins.

func (c *Conn) pin(id mumble.Identity, req request) {
	h := c.hub
	m, err := h.store.MessageByID(req.Message)
	if err != nil {
		c.sendError("unknown message")
		return
	}
	ch, ok := h.chatOf(m.ChannelID)
	if !ok || !h.may(id, ch, false) {
		c.sendError("unknown message")
		return
	}
	if !ch.direct && !h.mayModerate(id, ch) {
		c.sendError("only moderators pin messages")
		return
	}
	var at time.Time // zero: unpin
	if req.On {
		at = h.now()
	}
	m, changed, err := h.store.SetPinned(m.ID, at)
	switch {
	case errors.Is(err, store.ErrTooManyPins):
		c.sendError(err.Error())
	case err != nil:
		c.sendError("cannot pin the message")
	case changed:
		h.sendTo(ch, map[string]any{"type": "message-pinned", "message": h.toWire(m)})
	}
}

func (c *Conn) pins(req request) {
	msgs, err := c.hub.store.Pinned(req.Channel)
	if err != nil {
		c.sendError("cannot load the pinned messages")
		return
	}
	wire := make([]wireMessage, len(msgs))
	for i, m := range msgs {
		wire[i] = c.hub.toWireFor(m, c.viewer())
	}
	c.send(map[string]any{"type": "pins", "channel": req.Channel, "messages": wire})
}
