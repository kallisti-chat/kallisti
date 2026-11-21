package sidecar

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"time"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// recoveryValid: how long an admin's recovery link works.
const recoveryValid = 24 * time.Hour

// recoveryLink makes a one-time link to set a new password for a
// registered user's Kallisti account (see Voice.MayRecover; the link is
// used at /api/recover). A registration without an account (imported from
// Murmur, or made in a Mumble app) gets a claim link: using it makes the
// account, which takes over the registration.
func (c *Conn) recoveryLink(id mumble.Identity, user uint32) {
	h := c.hub
	if h.voice == nil {
		c.manageError("this needs the built-in Mumble server")
		return
	}
	name, claim, err := h.voice.MayRecover(id.Session, user)
	if err != nil {
		c.manageError(err.Error())
		return
	}
	fail := func(err error) {
		slog.Error("recovery link", "err", err)
		c.manageError("cannot make a recovery link")
	}
	var random [24]byte
	rand.Read(random[:])
	token := base64.RawURLEncoding.EncodeToString(random[:])
	if claim != "" {
		if err := h.store.SetClaimLink(store.ClaimLink{User: user, Name: name, CertHash: claim}, token, h.now().Add(recoveryValid)); err != nil {
			fail(err)
			return
		}
		slog.Info("claim link made", "admin", id.Name, "user", name)
		c.send(map[string]any{"type": "recovery-link", "user": user, "token": token})
		return
	}
	acc, err := h.store.AccountByName(name)
	if errors.Is(err, store.ErrNotFound) || err == nil && acc.Temporary {
		c.manageError("this user has no Kallisti account with a password to recover")
		return
	}
	if err != nil {
		fail(err)
		return
	}
	if err := h.store.SetRecoveryLink(acc.ID, token, h.now().Add(recoveryValid)); err != nil {
		fail(err)
		return
	}
	slog.Info("recovery link made", "admin", id.Name, "account", acc.Name)
	c.send(map[string]any{"type": "recovery-link", "user": user, "token": token})
}
