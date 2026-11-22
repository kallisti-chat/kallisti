package sidecar

import (
	"errors"
	"log/slog"
	"strconv"

	"kallisti.chat/server/internal/emoji"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Custom server emoji: admins and moderators add them (an upload token
// here, then POST /api/emoji, see files) and delete them. Everyone gets
// the list in ready and again after each change.

type wireEmoji struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// EmojiURL is where the server serves a custom emoji's image.
func EmojiURL(id int64) string { return "/api/emoji/" + strconv.FormatInt(id, 10) }

func (h *Hub) loadEmoji() error {
	list, err := h.store.Emojis()
	if err != nil {
		return err
	}
	wire := make([]wireEmoji, len(list)) // never null in JSON
	for i, e := range list {
		wire[i] = wireEmoji{Name: e.Name, URL: EmojiURL(e.ID)}
	}
	h.mu.Lock()
	h.emoji = wire
	h.mu.Unlock()
	return nil
}

func (h *Hub) customEmoji() []wireEmoji {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.emoji
}

func (h *Hub) hasEmoji(name string) bool {
	for _, e := range h.customEmoji() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// EmojiChanged sends everyone the custom emoji (after an upload). One
// change at a time, so the newest list is the one kept.
func (h *Hub) EmojiChanged() {
	h.emojiMu.Lock()
	defer h.emojiMu.Unlock()
	if err := h.loadEmoji(); err != nil {
		slog.Error("load custom emoji", "err", err)
		return
	}
	h.broadcast(map[string]any{"type": "emoji", "emoji": h.customEmoji()})
}

// mayManageEmoji answers with an error if the user may not.
func (c *Conn) mayManageEmoji(id mumble.Identity) bool {
	if c.hub.voice == nil || !c.hub.voice.MayManage(id.Session) {
		c.manageError("you may not manage emoji")
		return false
	}
	return true
}

// emojiToken gives a token for one emoji upload with this name (the
// upload checks the name again).
func (c *Conn) emojiToken(id mumble.Identity, name string) {
	if !c.mayManageEmoji(id) {
		return
	}
	switch {
	case !emoji.ValidName(name):
		c.manageError("an emoji name has 2 to 32 lower case letters, digits or _")
	case c.hub.hasEmoji(name):
		c.manageError("an emoji with this name exists")
	case len(c.hub.customEmoji()) >= store.MaxEmoji:
		c.manageError("this server has the most emoji it can have")
	default:
		c.send(map[string]any{"type": "upload-token", "token": c.hub.issueUploadToken(id.Name, name)})
	}
}

func (c *Conn) deleteEmoji(id mumble.Identity, name string) {
	if !c.mayManageEmoji(id) {
		return
	}
	err := c.hub.store.DeleteEmoji(name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.manageError("no such emoji")
	case err != nil:
		c.manageError("cannot delete the emoji")
	default:
		c.hub.EmojiChanged()
	}
}

// RedeemEmojiToken returns who may add which emoji with a token (once,
// before it expires).
func (h *Hub) RedeemEmojiToken(token string) (author, name string, ok bool) {
	t, ok := h.redeem(token, emojiToken)
	return t.author, t.emoji, ok
}
