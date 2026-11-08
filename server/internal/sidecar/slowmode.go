package sidecar

import (
	"cmp"
	"errors"
	"fmt"
	"sync"
	"time"

	"kallisti.chat/server/internal/mumble"
)

// Slow mode (built-in server): in a text channel, each user may post once
// per so many seconds; staff and the channel's moderators are exempt. Web posts (uploads too) and
// Mumble clients' texts to the channel's mirror take the same turn (see
// MumbleTextAllowed). Edits and reactions are no posts. Last posts live in
// memory: a restart forgets them.

// slowModes are the choices in seconds (0: off; an hour at most).
var slowModes = []int64{0, 5, 30, 60, 300, 3600}

// slowPosts: the last post per channel and user (registered users by id,
// guests by session).
type slowPosts struct {
	mu   sync.Mutex
	last map[slowKey]time.Time
}

type slowKey struct {
	channel    int64
	registered bool
	id         uint32
}

// slowLeft: how long a user who last posted at last still waits.
func slowLeft(last time.Time, every time.Duration, now time.Time) time.Duration {
	return max(last.Add(every).Sub(now), 0)
}

// slowText is the refusal, in whole seconds rounded up.
func slowText(wait time.Duration) string {
	return fmt.Sprintf("Slow mode: wait %d s", (wait+time.Second-1)/time.Second)
}

// slowTurn takes the user's turn to post in a text channel, or tells how
// long it waits (and until when). Posts count also while slow mode is
// off: one turned on holds back who just posted (as the composer does).
// A thread has its channel's slow mode, and turns of its own. If accept is
// set, it must also accept the post before the turn is recorded.
func (h *Hub) slowTurn(id mumble.Identity, ch chat, accept func() error) (time.Duration, time.Time, error) {
	if accept == nil {
		accept = func() error { return nil }
	}
	if h.voice == nil || ch.direct || ch.isVoice || h.mayModerate(id, ch) {
		return 0, time.Time{}, accept()
	}
	every := time.Duration(h.slowModeOf(cmp.Or(ch.parent, ch.id))) * time.Second
	key := slowKey{channel: ch.id, registered: id.Registered, id: id.Session}
	if id.Registered {
		key.id = id.UserID
	}
	now := h.now()
	p := &h.slow
	p.mu.Lock()
	defer p.mu.Unlock()
	if wait := slowLeft(p.last[key], every, now); wait > 0 {
		return wait, p.last[key].Add(every), nil
	}
	if err := accept(); err != nil {
		return 0, time.Time{}, err
	}
	if p.last == nil {
		p.last = map[slowKey]time.Time{}
	}
	for k, t := range p.last { // nobody waits longer than an hour
		if now.Sub(t) >= time.Hour {
			delete(p.last, k)
		}
	}
	p.last[key] = now
	return 0, time.Time{}, nil
}

func (h *Hub) slowModeOf(channel int64) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.channels {
		if c.ID == channel {
			return c.SlowMode
		}
	}
	return 0
}

// slowMode refuses a post that comes too soon (see post); its text comes
// back for the composer.
func (c *Conn) slowMode(id mumble.Identity, ch chat, text string) bool {
	return c.postLimits(id, ch, text, nil)
}

// postLimits checks slow mode and the optional account limit together:
// neither records a post if the other refuses it. accept must not reenter
// slow mode, as slowTurn calls it with the slow-post lock held.
func (c *Conn) postLimits(id mumble.Identity, ch chat, text string, accept func() error) bool {
	wait, until, err := c.hub.slowTurn(id, ch, accept)
	if err != nil {
		c.send(map[string]any{"type": "error", "message": err.Error(), "about": "refused", "channel": ch.id, "text": text})
		return true
	}
	if wait > 0 {
		c.send(map[string]any{"type": "error", "message": slowText(wait), "about": "slow-mode", "channel": ch.id, "until": until.UnixMilli(), "text": text})
	}
	return wait > 0
}

// MumbleTextAllowed takes the turn of a Mumble protocol text to a channel
// (see mumbled.Config.CheckChannelText), also a web client's binary one
// (by its account): slow mode holds it in text channel mirrors.
func (h *Hub) MumbleTextAllowed(session, channel uint32) error {
	text, ok := h.textOf(channel)
	if !ok || h.voice == nil {
		return nil
	}
	u, _ := h.voice.MumbleClient(session)
	if wait, _, _ := h.slowTurn(mumble.Identity{Session: session, Registered: u.Registered, UserID: u.UserID}, chat{id: text.ID}, nil); wait > 0 {
		return errors.New(slowText(wait))
	}
	return nil
}
