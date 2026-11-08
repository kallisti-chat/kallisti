package sidecar

import (
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/store"
)

// Editing and deleting messages. Registered users own the messages with
// their user id (messages from before ids were kept: their verified name);
// guests (whose names are free) only those they wrote in their current
// connection. Moderators and admins delete any message.

// Guest messages remembered, at most (oldest forgotten first is not
// needed: a full map starts over).
const maxGuestMessages = 10_000

func (h *Hub) rememberGuest(message int64, session uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.guestMessages == nil || len(h.guestMessages) >= maxGuestMessages {
		h.guestMessages = map[int64]uint32{}
	}
	h.guestMessages[message] = session
}

func (h *Hub) wrote(id mumble.Identity, m store.Message) bool {
	if m.Verified {
		return byUser(id, m)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	session, ok := h.guestMessages[m.ID]
	return ok && !id.Registered && session == id.Session
}

// byUser: a registered user's own message (see wrote).
func byUser(id mumble.Identity, m store.Message) bool {
	if !id.Registered || !m.Verified {
		return false
	}
	if m.AuthorID != nil {
		return *m.AuthorID == id.UserID
	}
	return names.Key(id.Name) == names.Key(m.Author)
}

func (c *Conn) changeMessage(id mumble.Identity, req request) {
	h := c.hub
	m, err := h.store.MessageByID(req.Message)
	if err != nil {
		c.sendError("unknown message")
		return
	}
	ch, ok := h.chatOf(m.ChannelID)
	if !ok || !h.may(id, ch, req.Type == "edit-message") {
		c.sendError("unknown message")
		return
	}
	send := func(v any) { h.sendTo(ch, v) }
	own := h.wrote(id, m)
	if req.Type == "edit-message" {
		kept, gone, known := splitUploads(m.Uploads, req.Remove)
		text, valid := validText(req.Text, len(kept) > 0)
		switch {
		case !own:
			c.sendError("you may only edit your own messages")
		case m.Sticker != nil:
			c.sendError("a sticker cannot be edited")
		case m.Poll != nil:
			c.sendError("a poll cannot be edited")
		case !known:
			c.sendError("unknown upload")
		case !valid:
			c.sendError(textError)
		default:
			// No new pings by an edit; one taken out is gone.
			all := m.MentionAll
			if all != "" && mentionAllOf(text) != all {
				all = ""
			}
			ids := make([]int64, len(gone))
			for i, u := range gone {
				ids[i] = u.ID
			}
			edited, err := h.store.EditMessage(m.ID, text, all, h.now(), ids...)
			// The files of the rows that went (also if reading it back failed).
			for _, u := range gone {
				if _, e := h.store.UploadByKey(u.Key); errors.Is(e, store.ErrNotFound) {
					h.deleteFiles(u.Key)
				}
			}
			if errors.Is(err, store.ErrEmpty) {
				c.sendError(textError)
				return
			}
			if err != nil {
				c.sendError("cannot edit the message")
				return
			}
			m = edited
			send(map[string]any{"type": "message-edited", "message": h.toWire(m)})
			if req.RequestID != "" && len(req.RequestID) <= maxNonce {
				c.send(map[string]any{"type": "edit-result", "requestId": req.RequestID, "message": m.ID})
			}
			if m.Thread != nil { // its quote
				h.threadChanged(m.Thread.ID)
			}
		}
		return
	}
	if !own && !h.mayModerate(id, ch) {
		c.sendError("you may only delete your own messages")
		return
	}
	if m, err = h.store.DeleteMessage(m.ID); err != nil {
		c.sendError("cannot delete the message")
		return
	}
	h.removed([]store.Message{m})
}

// Deleting a user's recent messages at once (staff, see Voice.MessagesOf):
// at most maxPurge, the newest first, of up to a week, in text channels and
// voice channel chats. Direct chats stay private. Registered users by id
// (offline too), guests only what they wrote in their connection (see
// wrote): a kick must come after.
const (
	maxPurge        = 1000
	maxPurgeSeconds = 7 * 24 * 3600
)

func (c *Conn) deleteMessagesOf(id mumble.Identity, req request) {
	h := c.hub
	if h.voice == nil {
		c.manageError("deleting messages at once needs the built-in Mumble server")
		return
	}
	if req.Seconds <= 0 || req.Seconds > maxPurgeSeconds {
		c.manageError("invalid time range")
		return
	}
	registered, who, err := h.voice.MessagesOf(id.Session, req.Session, req.User)
	if err != nil {
		c.manageError(err.Error())
		return
	}
	since := h.now().Add(-time.Duration(req.Seconds) * time.Second)
	var ids []int64
	if registered {
		ids, err = h.store.RecentByUser(who, since, maxPurge)
	} else {
		ids, err = h.store.RecentAmong(h.guestMessagesOf(who), since, maxPurge)
	}
	var gone []store.Message
	if err == nil {
		gone, err = h.store.DeleteMessages(ids)
	}
	if err != nil {
		c.manageError("cannot delete the messages")
		return
	}
	h.removed(gone)
}

func (h *Hub) guestMessagesOf(session uint32) []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []int64
	for m, s := range h.guestMessages {
		if s == session {
			out = append(out, m)
		}
	}
	return out
}

// removed deletes the files of deleted messages and tells each chat's
// readers: one id as "message-deleted" (what older clients know), more as
// one "messages-deleted".
func (h *Hub) removed(gone []store.Message) {
	byChat := map[int64][]int64{}
	for _, m := range gone {
		for _, u := range m.Uploads {
			h.deleteFiles(u.Key)
		}
		byChat[m.ChannelID] = append(byChat[m.ChannelID], m.ID)
	}
	for channel, ids := range byChat {
		ch, ok := h.chatOf(channel)
		switch {
		case !ok:
		case len(ids) == 1:
			h.sendTo(ch, map[string]any{"type": "message-deleted", "channel": channel, "id": ids[0]})
		default:
			h.sendTo(ch, map[string]any{"type": "messages-deleted", "channel": channel, "ids": ids})
		}
		if ok && ch.parent != 0 { // fewer replies
			h.threadChanged(channel)
		}
	}
	// Threads stay without their start messages.
	starts := make([]int64, len(gone))
	for i, m := range gone {
		starts[i] = m.ID
	}
	threads, err := h.store.ThreadsFrom(starts)
	if err != nil {
		slog.Error("threads of deleted messages", "err", err)
	}
	for _, t := range threads {
		h.threadChanged(t.ID)
	}
}

// splitUploads: the uploads an edit keeps and those it takes off (by key);
// known is false if a key is not one of them.
func splitUploads(uploads []store.Upload, remove []string) (kept, gone []store.Upload, known bool) {
	for _, u := range uploads {
		if slices.Contains(remove, u.Key) {
			gone = append(gone, u)
		} else {
			kept = append(kept, u)
		}
	}
	known = !slices.ContainsFunc(remove, func(k string) bool {
		return !slices.ContainsFunc(uploads, func(u store.Upload) bool { return u.Key == k })
	})
	return kept, gone, known
}

// deleteFiles removes the files of uploads whose rows are gone.
func (h *Hub) deleteFiles(keys ...string) {
	for _, k := range keys {
		if h.OnUploadDeleted != nil {
			h.OnUploadDeleted(k)
		}
	}
}

// A message has 1 to MaxTextLength characters; with an attachment it may
// have none.
const textError = "message must be 1 to 2000 characters"

func validText(raw string, hasUpload bool) (string, bool) {
	text := strings.TrimSpace(raw)
	return text, (text != "" || hasUpload) && utf8.RuneCountInString(text) <= MaxTextLength
}
