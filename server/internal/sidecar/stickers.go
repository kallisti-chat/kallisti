package sidecar

import (
	"errors"
	"log/slog"
	"strconv"
	"unicode/utf8"

	"kallisti.chat/server/internal/emoji"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Stickers: large pictures sent as a message of their own. Who may manage
// emoji adds them (an upload token here, then POST /api/stickers, see
// files), renames and deletes them. Everyone gets the list in ready and
// again after each change.

type wireSticker struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Emoji       string `json:"emoji,omitempty"`
	Pack        string `json:"pack,omitempty"`
	URL         string `json:"url"`
}

const (
	maxStickerDescription = 100 // characters
	maxStickerPack        = 32
)

// StickerURL is where the server serves a sticker's image.
func StickerURL(id int64) string { return "/api/stickers/" + strconv.FormatInt(id, 10) }

func (h *Hub) loadStickers() error {
	list, err := h.store.Stickers()
	if err != nil {
		return err
	}
	wire := make([]wireSticker, len(list)) // never null in JSON
	for i, s := range list {
		wire[i] = wireSticker{ID: s.ID, Name: s.Name, Description: s.Description, Emoji: s.Emoji, Pack: s.Pack, URL: StickerURL(s.ID)}
	}
	h.mu.Lock()
	h.stickers = wire
	h.mu.Unlock()
	return nil
}

func (h *Hub) stickerList() []wireSticker {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stickers
}

// StickersChanged sends everyone the stickers (after a change). One
// change at a time, so the newest list is the one kept.
func (h *Hub) StickersChanged() {
	h.stickersMu.Lock()
	defer h.stickersMu.Unlock()
	if err := h.loadStickers(); err != nil {
		slog.Error("load stickers", "err", err)
		return
	}
	h.broadcast(map[string]any{"type": "stickers", "stickers": h.stickerList()})
}

// stickerFields checks a sticker's name, description, emoji and pack ("":
// they are fine), the name against the others but the sticker itself.
func (h *Hub) stickerFields(req request, self int64) string {
	switch {
	case !emoji.ValidName(req.Name):
		return "a sticker name has 2 to 32 lower case letters, digits or _"
	case utf8.RuneCountInString(req.Description) > maxStickerDescription:
		return "a sticker description has at most 100 characters"
	case utf8.RuneCountInString(req.Pack) > maxStickerPack:
		return "a pack name has at most 32 characters"
	case req.Emoji != "" && !emoji.Valid(emoji.Normal(req.Emoji)):
		return "the related emoji is not an emoji"
	}
	for _, s := range h.stickerList() {
		if s.Name == req.Name && s.ID != self {
			return "a sticker with this name exists"
		}
	}
	return ""
}

func stickerOf(req request) store.Sticker {
	e := req.Emoji
	if e != "" {
		e = emoji.Normal(e)
	}
	return store.Sticker{ID: req.Sticker, Name: req.Name, Description: req.Description, Emoji: e, Pack: req.Pack}
}

// stickerRequest: sticker-token, rename-sticker, delete-sticker.
func (c *Conn) stickerRequest(id mumble.Identity, req request) {
	if !c.mayManageEmoji(id) {
		return
	}
	switch req.Type {
	case "sticker-token":
		if msg := c.hub.stickerFields(req, 0); msg != "" {
			c.manageError(msg)
		} else if len(c.hub.stickerList()) >= store.MaxStickers {
			c.manageError("this server has the most stickers it can have")
		} else {
			s := stickerOf(req)
			s.Author = id.Name
			c.send(map[string]any{"type": "upload-token", "token": c.hub.issueToken(uploadToken{kind: stickerToken, author: id.Name, sticker: &s})})
		}
	case "rename-sticker":
		if msg := c.hub.stickerFields(req, req.Sticker); msg != "" {
			c.manageError(msg)
			return
		}
		c.stickerDone(c.hub.store.UpdateSticker(stickerOf(req)))
	case "delete-sticker":
		c.stickerDone(c.hub.store.DeleteSticker(req.Sticker))
	}
}

func (c *Conn) stickerDone(err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.manageError("no such sticker")
	case errors.Is(err, store.ErrNameTaken):
		c.manageError("a sticker with this name exists")
	case err != nil:
		c.manageError("cannot change the sticker")
	default:
		c.hub.StickersChanged()
	}
}

// RedeemStickerToken returns the sticker a token may add (once, before
// it expires): its fields and author.
func (h *Hub) RedeemStickerToken(token string) (store.Sticker, bool) {
	t, ok := h.redeem(token, stickerToken)
	if !ok {
		return store.Sticker{}, false
	}
	return *t.sticker, true
}

// stickerText is a sticker message's text: what clients that show only
// text show.
func stickerText(name string) string { return "[sticker: " + name + "]" }
