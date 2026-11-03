package sidecar

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"time"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Registration invitations admit an account to the server. Only admins
// manage them; channel moderators and channel invites grant no admission.
func (c *Conn) registrationRequest(id mumble.Identity, req request) {
	h := c.hub
	if h.voice == nil || !h.voice.IsAdmin(id.Session) {
		c.manageError("only admins manage account invitations")
		return
	}
	fail := func(err error) bool {
		if err == nil {
			return false
		}
		slog.Error("registration settings", "err", err)
		c.manageError("cannot save registration settings")
		return true
	}
	switch req.Type {
	case "set-invite-only":
		if !fail(h.store.SetInviteOnly(req.On)) {
			h.broadcast(map[string]any{"type": "invite-only", "on": req.On})
		}
		return
	case "create-registration-invite":
		if req.Seconds < 0 || req.Seconds > int64(maxInviteValid/time.Second) || req.Uses < 0 || req.Uses > maxInviteUses {
			c.manageError("invalid account invitation")
			return
		}
		var random [24]byte
		rand.Read(random[:])
		token := base64.RawURLEncoding.EncodeToString(random[:])
		now := h.now()
		i := store.RegistrationInvite{Creator: id.Name, Created: now.UnixMilli(), MaxUses: req.Uses}
		if req.Seconds != 0 {
			i.Expires = now.Add(time.Duration(req.Seconds) * time.Second).UnixMilli()
		}
		if fail(h.store.AddRegistrationInvite(token, i)) {
			return
		}
		c.send(map[string]any{"type": "registration-invite-created", "token": token})
	case "delete-registration-invite":
		if fail(h.store.DeleteRegistrationInvite(req.Invite)) {
			return
		}
	}
	list, err := h.store.RegistrationInvites()
	if !fail(err) {
		c.send(map[string]any{"type": "registration-invites", "invites": list})
	}
}
