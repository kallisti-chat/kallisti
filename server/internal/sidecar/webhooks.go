package sidecar

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Webhooks post into a text channel from outside (see webhookpost.go):
// admins and moderators make them. The URL holds a random token (192
// bits, as invite links); the store keeps only its hash, so only the
// maker sees the URL, once.

const (
	maxWebhooks    = 10 // per channel
	maxWebhookName = 80 // characters, as Discord
)

type wireWebhook struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Creator string `json:"creator"`
	Created int64  `json:"created"` // Unix ms
	Picture bool   `json:"picture,omitempty"`
}

// webhookName is a name as kept (false: empty, too long, or with control
// characters).
func webhookName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n > 0 && n <= maxWebhookName && !strings.ContainsFunc(s, unicode.IsControl)
}

func (c *Conn) sendWebhooks(channel int64) {
	list, err := c.hub.store.Webhooks(channel)
	if err != nil {
		slog.Error("list webhooks", "err", err)
		c.manageError("cannot list the webhooks")
		return
	}
	out := make([]wireWebhook, len(list))
	for i, w := range list {
		out[i] = wireWebhook{ID: w.ID, Name: w.Name, Creator: w.Creator, Created: w.CreatedAt.UnixMilli(), Picture: w.Picture}
	}
	c.send(map[string]any{"type": "webhooks", "channel": channel, "webhooks": out})
}

// webhookRequest answers the webhook requests: webhooks (the list of a
// channel), create-webhook, rename-webhook, delete-webhook,
// webhook-picture-token (one upload, see files' webhook pictures),
// remove-webhook-picture.
func (c *Conn) webhookRequest(id mumble.Identity, req request) {
	h := c.hub
	// As channel management: a channel deleted meanwhile gets no webhook.
	h.manageMu.Lock()
	defer h.manageMu.Unlock()
	channel := req.Channel
	if h.voice == nil || !h.voice.MayManage(id.Session) { // first: no hint which ids exist
		c.manageError("only admins and moderators manage webhooks")
		return
	}
	if req.Type != "webhooks" && req.Type != "create-webhook" {
		var err error
		if channel, err = h.store.WebhookChannel(req.Webhook); err != nil {
			c.manageError("no such webhook")
			return
		}
	}
	if !h.channelExists(channel) {
		c.manageError("webhooks are for text channels")
		return
	}
	name, ok := webhookName(req.Name)
	if !ok && (req.Type == "create-webhook" || req.Type == "rename-webhook") {
		c.manageError("a webhook's name has 1 to 80 characters")
		return
	}
	switch req.Type {
	case "create-webhook":
		b := make([]byte, 24)
		rand.Read(b)
		token := base64.RawURLEncoding.EncodeToString(b)
		w, err := h.store.AddWebhook(inviteHash(token), store.Webhook{Channel: channel, Name: name, Creator: id.Name, CreatedAt: h.now()}, maxWebhooks)
		if errors.Is(err, store.ErrTooManyWebhooks) {
			c.manageError("at most 10 webhooks per channel")
			return
		} else if err != nil {
			slog.Error("add webhook", "err", err)
			c.manageError("cannot make the webhook")
			return
		}
		c.send(map[string]any{"type": "webhook-created", "channel": channel, "id": w.ID, "token": token})
	case "rename-webhook":
		if _, err := h.store.RenameWebhook(req.Webhook, name); err != nil {
			c.manageError("no such webhook")
			return
		}
	case "delete-webhook":
		if _, err := h.store.DeleteWebhook(req.Webhook); err != nil {
			c.manageError("no such webhook")
			return
		}
		h.webhookLimits.forget(req.Webhook)
	case "webhook-picture-token":
		c.send(map[string]any{"type": "upload-token", "token": h.issueToken(uploadToken{kind: webhookPictureToken, author: id.Name, webhook: req.Webhook})})
		return
	case "remove-webhook-picture":
		if _, err := h.store.SetWebhookPicture(req.Webhook, nil, h.now()); err != nil {
			c.manageError("no such webhook")
			return
		}
		h.WebhookPictureChanged(req.Webhook)
	}
	c.sendWebhooks(channel)
}

// RedeemWebhookPictureToken says which webhook's picture a token uploads
// (once, before it expires).
func (h *Hub) RedeemWebhookPictureToken(token string) (webhook int64, ok bool) {
	t, ok := h.redeem(token, webhookPictureToken)
	return t.webhook, ok
}

// WebhookPictureChanged tells everyone a webhook's picture changed (set or
// removed): its posts' avatars load it anew (the version in the address).
func (h *Hub) WebhookPictureChanged(webhook int64) {
	h.broadcast(map[string]any{"type": "webhook-picture", "webhook": webhook, "version": h.now().UnixMilli()})
}
