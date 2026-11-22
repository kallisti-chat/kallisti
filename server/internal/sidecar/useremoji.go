package sidecar

import (
	"errors"
	"log/slog"
	"slices"
	"strconv"

	"kallisti.chat/server/internal/emoji"
	"kallisti.chat/server/internal/msgrules"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Personal emoji: registered users (not guests) keep their own list of
// emoji (see store/useremoji.go). Messages and reactions refer to one as
// "<:name:id>" (emoji.Ref), so everyone sees it; only its owner uses it.
// A new image comes as an upload (a token here, then POST
// /api/emoji/personal, see files); an image in a message is added here.
// Moderators remove an image everywhere; clients then show the
// references as text.

type wireUserEmoji struct {
	Name string `json:"name"`
	ID   int64  `json:"id"` // the image, at UserEmojiURL
}

// UserEmojiURL is where the server serves a personal emoji's image.
func UserEmojiURL(id int64) string { return "/api/emoji/personal/" + strconv.FormatInt(id, 10) }

// userEmojiFor is the user's list (never null in JSON).
func (h *Hub) userEmojiFor(id mumble.Identity) []wireUserEmoji {
	out := []wireUserEmoji{}
	if !id.Registered {
		return out
	}
	list, err := h.store.UserEmojis(id.UserID)
	if err != nil {
		slog.Error("personal emoji", "err", err)
	}
	for _, e := range list {
		out = append(out, wireUserEmoji{Name: e.Name, ID: e.ImageID})
	}
	return out
}

// removedEmoji: the images moderators removed (never null in JSON).
func (h *Hub) removedEmoji() []int64 {
	ids, err := h.store.RemovedEmojiImages()
	if err != nil {
		slog.Error("removed emoji", "err", err)
	}
	return append([]int64{}, ids...)
}

// UserEmojiChanged sends a user's connections the user's list. One change
// at a time (uploads come from other goroutines), so the newest list is
// the one kept.
func (h *Hub) UserEmojiChanged(user uint32) {
	h.emojiMu.Lock()
	defer h.emojiMu.Unlock()
	var list []wireUserEmoji
	h.eachReady(func(c *Conn, id mumble.Identity) {
		if id.Registered && id.UserID == user {
			if list == nil {
				list = h.userEmojiFor(id)
			}
			c.send(map[string]any{"type": "my-emoji", "emoji": list})
		}
	})
}

func (c *Conn) userEmoji(id mumble.Identity, req request) {
	if req.Type == "remove-emoji-image" {
		c.removeEmojiImage(id, req.Emoji)
		return
	}
	if !id.Registered || c.hub.isGuest()(id.Name) {
		c.manageError("make an account to add your own emoji")
		return
	}
	switch req.Type {
	case "user-emoji-token":
		c.userEmojiToken(id, req.Name)
	case "add-user-emoji":
		c.addUserEmoji(id, req.Emoji, req.Name)
	case "rename-user-emoji":
		if c.userEmojiNameOK(id, req.To, false) {
			c.userEmojiDone(id, c.hub.store.RenameUserEmoji(id.UserID, req.Name, req.To))
		}
	case "delete-user-emoji":
		c.userEmojiDone(id, c.hub.store.DeleteUserEmoji(id.UserID, req.Name))
	}
}

// userEmojiNameOK answers with an error if a name cannot be one of the
// user's emoji: server emoji keep their names. adding: a new one (the
// limit counts).
func (c *Conn) userEmojiNameOK(id mumble.Identity, name string, adding bool) bool {
	mine := c.hub.userEmojiFor(id)
	switch {
	case !emoji.ValidName(name):
		c.manageError("an emoji name has 2 to 32 lower case letters, digits or _")
	case c.hub.hasEmoji(name):
		c.manageError("this server has an emoji with this name")
	case slices.ContainsFunc(mine, func(e wireUserEmoji) bool { return e.Name == name }):
		c.manageError("you have an emoji with this name")
	case adding && len(mine) >= store.MaxUserEmoji:
		c.manageError("you have the most emoji you can have (" + strconv.Itoa(store.MaxUserEmoji) + ")")
	default:
		return true
	}
	return false
}

// userEmojiToken gives a token for one upload of a new image (the upload
// checks again). Uploads follow the message rules for new accounts.
func (c *Conn) userEmojiToken(id mumble.Identity, name string) {
	if !c.userEmojiNameOK(id, name, true) {
		return
	}
	if c.hub.voice != nil {
		if _, err := c.hub.voice.CheckMessage(id.Session, msgrules.Message{Upload: true}); err != nil {
			c.manageError(err.Error())
			return
		}
	}
	c.send(map[string]any{"type": "upload-token", "token": c.hub.issueToken(uploadToken{kind: userEmojiToken, author: id.Name, emoji: name, user: id.UserID})})
}

// addUserEmoji adds an emoji someone used (a personal one, "<:name:id>",
// or a server emoji, ":name:") under a name of the user's.
func (c *Conn) addUserEmoji(id mumble.Identity, from, name string) {
	if !c.userEmojiNameOK(id, name, true) {
		return
	}
	at := c.hub.now()
	if _, image, ok := emoji.ParseRef(from); ok {
		_, err := c.hub.store.AddUserEmojiImage(id.UserID, name, image, at)
		c.userEmojiDone(id, err)
		return
	}
	server, ok := emoji.CustomName(from)
	if !ok {
		c.manageError("no such emoji")
		return
	}
	list, err := c.hub.store.Emojis()
	if err != nil {
		c.manageError("cannot add the emoji")
		return
	}
	i := slices.IndexFunc(list, func(e store.Emoji) bool { return e.Name == server })
	if i < 0 {
		c.manageError("no such emoji")
		return
	}
	e, err := c.hub.store.EmojiByID(list[i].ID)
	if err == nil {
		_, err = c.hub.store.AddUserEmoji(id.UserID, name, e.Type, e.Data, at)
	}
	c.userEmojiDone(id, err)
}

// userEmojiDone answers a change of the user's emoji: the new list to
// the user's connections, or why not.
func (c *Conn) userEmojiDone(id mumble.Identity, err error) {
	if err != nil {
		c.manageError(UserEmojiError(err))
		return
	}
	c.hub.UserEmojiChanged(id.UserID)
}

// UserEmojiError is a store error for the user (also for uploads).
func UserEmojiError(err error) string {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "no such emoji"
	case errors.Is(err, store.ErrNameTaken):
		return "you have an emoji with this name"
	case errors.Is(err, store.ErrEmojiOwned):
		return "you have this emoji already"
	case errors.Is(err, store.ErrEmojiBlocked):
		return "a moderator removed this emoji image"
	case errors.Is(err, store.ErrTooManyEmoji):
		return "you have the most emoji you can have (" + strconv.Itoa(store.MaxUserEmoji) + ")"
	case errors.Is(err, store.ErrTooManyNewEmoji):
		return "you added many new emoji today, try again tomorrow"
	}
	slog.Error("personal emoji", "err", err)
	return "cannot change your emoji"
}

// removeEmojiImage removes a personal emoji's image everywhere and blocks
// it (admins and moderators, as for server emoji).
func (c *Conn) removeEmojiImage(id mumble.Identity, ref string) {
	if !c.mayManageEmoji(id) {
		return
	}
	_, image, ok := emoji.ParseRef(ref)
	if !ok {
		c.manageError("no such emoji")
		return
	}
	owners, err := c.hub.store.RemoveEmojiImage(image)
	if err != nil {
		c.manageError(UserEmojiError(err))
		return
	}
	slog.Info("emoji image removed", "image", image, "by", id.Name)
	c.hub.emojiMu.Lock() // the newest list last
	c.hub.broadcast(map[string]any{"type": "removed-emoji", "ids": c.hub.removedEmoji()})
	c.hub.emojiMu.Unlock()
	for _, u := range owners {
		c.hub.UserEmojiChanged(u)
	}
}

// RedeemUserEmojiToken returns who may add which personal emoji with a
// token (once, before it expires).
func (h *Hub) RedeemUserEmojiToken(token string) (user uint32, name string, ok bool) {
	t, ok := h.redeem(token, userEmojiToken)
	return t.user, t.emoji, ok
}

// reactionEmoji is the form of a personal emoji reaction: the image's
// first name, so everyone's reactions with an image are one. Adding one
// needs an image that was not removed.
func (h *Hub) reactionEmoji(ref string, on bool) (string, bool) {
	_, image, ok := emoji.ParseRef(ref)
	if !ok {
		return "", false
	}
	name, removed, err := h.store.EmojiImageName(image)
	if err != nil || on && removed {
		return "", false
	}
	return emoji.Ref(name, image), true
}
