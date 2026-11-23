package sidecar

import (
	"log/slog"

	"kallisti.chat/server/internal/mumble"
)

// The server's logo (admins): a token for one upload (files.Handler makes
// the icons and stores them), removal, and its version for everyone ("":
// none).

func (c *Conn) mayChangeLogo(id mumble.Identity) bool {
	if c.hub.voice == nil || !c.hub.voice.IsAdmin(id.Session) {
		c.manageError("only admins change the logo")
		return false
	}
	return true
}

func (c *Conn) logoToken(id mumble.Identity) {
	if c.mayChangeLogo(id) {
		c.send(map[string]any{"type": "upload-token", "token": c.hub.issueToken(uploadToken{kind: logoToken, author: id.Name})})
	}
}

// RedeemLogoToken returns who may upload a logo with a token (once,
// before it expires).
func (h *Hub) RedeemLogoToken(token string) (author string, ok bool) {
	t, ok := h.redeem(token, logoToken)
	return t.author, ok
}

func (c *Conn) removeLogo(id mumble.Identity) {
	if !c.mayChangeLogo(id) {
		return
	}
	if err := c.hub.store.DeleteLogo(); err != nil {
		c.manageError("cannot remove the logo")
		return
	}
	c.hub.LogoChanged()
}

// LogoChanged tells everyone the logo's version.
func (h *Hub) LogoChanged() {
	h.broadcast(map[string]any{"type": "logo", "version": h.logoVersion()})
}

func (h *Hub) logoVersion() string {
	v, err := h.store.LogoVersion()
	if err != nil {
		slog.Error("logo version", "err", err)
	}
	return v
}
