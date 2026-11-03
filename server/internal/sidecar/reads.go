package sidecar

import (
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/store"
)

// Read state for registered users: the newest message read per channel,
// and on ready a summary (newest message, unread mentions) per channel, so
// unread marks and mention badges survive reloads and other devices.
// Guests keep theirs in the browser only.

// mentionPattern is the web client's rule (lib/mentions.ts) on plain text:
// "@" plus the whole name, any case, after a space (as JavaScript's \s) or
// an opening mark.
func mentionPattern(name string) *regexp.Regexp {
	// Not followed by "@host": that is another server's account of the name.
	return regexp.MustCompile(`(?i)(?:^|[\s\v\p{Z}\x{FEFF}(\[{,:])@` + regexp.QuoteMeta(name) + `(?:$|[^\p{L}\p{N}_@.-]|[@.](?:$|[^\p{L}\p{N}]))`)
}

func mentionsName(text, name string) bool { return mentionPattern(name).MatchString(text) }

// mentionAll: @everyone first (it wins), then @here.
var mentionAll = []struct {
	name    string
	pattern *regexp.Regexp
}{{"everyone", mentionPattern("everyone")}, {"here", mentionPattern("here")}}

// mentionAllOf is "everyone" for @everyone, "here" for @here (moderators
// only: the caller checks), else "".
func mentionAllOf(text string) string {
	for _, all := range mentionAll {
		if all.pattern.MatchString(text) {
			return all.name
		}
	}
	return ""
}

type summaryEntry struct {
	Channel  int64   `json:"channel"`
	Voice    uint32  `json:"voice,omitempty"`  // a voice channel's chat
	Direct   *uint32 `json:"direct,omitempty"` // a direct chat: the other user (0 is SuperUser)
	Parent   int64   `json:"parent,omitempty"` // a thread: its text channel
	Latest   int64   `json:"latest"`
	Mentions int     `json:"mentions"`
}

// readableChats are the text channels (and their threads), the voice
// chats and the direct chats a user may read.
func (h *Hub) readableChats(id mumble.Identity) []summaryEntry {
	var chats []summaryEntry
	reads := map[int64]bool{}
	for _, c := range h.channelsFor(id) {
		chats = append(chats, summaryEntry{Channel: c.ID})
		reads[c.ID] = true
	}
	threads, err := h.store.ThreadParents()
	if err != nil {
		slog.Error("threads", "err", err)
	}
	for thread, parent := range threads {
		if reads[parent] {
			chats = append(chats, summaryEntry{Channel: thread, Parent: parent})
		}
	}
	if h.voice == nil {
		return chats
	}
	voiceChats, err := h.store.VoiceChats()
	if err != nil {
		slog.Error("voice chats", "err", err)
	}
	for chat, voice := range voiceChats {
		if h.voice.MayRead(id.Session, voice) {
			chats = append(chats, summaryEntry{Channel: chat, Voice: voice})
		}
	}
	if id.Registered {
		directs, err := h.store.DirectChats(id.UserID)
		if err != nil {
			slog.Error("direct chats", "err", err)
		}
		for chat, peer := range directs {
			chats = append(chats, summaryEntry{Channel: chat, Direct: &peer})
		}
	}
	return chats
}

// Search results, at most; search text, at most.
const (
	searchResults   = 25
	maxSearchLength = 200
)

// search finds messages in the chats the user may read (or in one of
// them), optionally by one author.
func (c *Conn) search(id mumble.Identity, req request) {
	if utf8.RuneCountInString(req.Text) > maxSearchLength || utf8.RuneCountInString(req.Author) > maxSearchLength {
		c.sendError("a search has at most 200 characters")
		return
	}
	author := strings.TrimSpace(req.Author)
	var ids []int64
	threads := map[int64]int64{}
	for _, e := range c.hub.readableChats(id) {
		if req.Channel == 0 || req.Channel == e.Channel {
			ids = append(ids, e.Channel)
		}
		if e.Parent != 0 {
			threads[e.Channel] = e.Parent
		}
	}
	// No words and no filter: nothing (not the newest messages).
	if !store.HasSearchWords(req.Text) && author == "" && req.Channel == 0 {
		ids = nil
	}
	msgs, err := c.hub.store.Search(req.Text, ids, author, searchResults)
	if err != nil {
		slog.Error("search", "err", err)
		c.sendError("cannot search")
		return
	}
	wire := make([]wireMessage, len(msgs))
	for i, m := range msgs {
		wire[i] = toWireFor(m, c.viewer())
	}
	c.send(map[string]any{"type": "search-results", "text": req.Text, "messages": wire, "threads": c.hub.threadsOf(msgs, threads)})
}

// readState is what ready carries for a registered user: read marks and a
// summary of the text channels and the voice chats it may read.
func (h *Hub) readState(id mumble.Identity) (map[int64]int64, []summaryEntry) {
	user := names.Key(id.Name)
	marks, err := h.store.ReadMarks(id.UserID)
	if err != nil {
		slog.Error("read marks", "err", err)
		return nil, nil
	}
	chats := h.readableChats(id)
	mention := mentionPattern(id.Name)
	followed := h.followsOf(id)
	out := chats[:0]
	for _, e := range chats {
		if e.Parent != 0 && !slices.Contains(followed, e.Channel) {
			continue // threads count for their followers
		}
		// In a direct chat every message counts (to the user, as a mention).
		contains := "@"
		if e.Direct != nil {
			contains = ""
		}
		latest, msgs, err := h.store.MessagesAfter(e.Channel, marks[e.Channel], contains)
		if err != nil {
			slog.Error("channel summary", "err", err)
			continue
		}
		e.Latest = latest
		for _, m := range msgs {
			repliesToMe := answers(m, user)
			// @here only reached who was there (live).
			all := m.MentionAll == "everyone"
			if !byUser(id, m) && (e.Direct != nil || all || mention.MatchString(m.Text) || repliesToMe) {
				e.Mentions++
			}
		}
		if e.Latest > 0 {
			out = append(out, e)
		}
	}
	return marks, out
}

// markRead stores a registered user's read mark (guests: nothing); back:
// mark unread from the message on (the mark goes to the one before).
func (c *Conn) markRead(id mumble.Identity, req request, back bool) {
	if !id.Registered || back && req.Message < 1 {
		return
	}
	ch, ok := c.hub.chatOf(req.Channel)
	if !ok || !c.hub.may(id, ch, false) {
		return
	}
	if back {
		c.hub.setReadMark(id.UserID, req.Channel, req.Message-1, true)
	} else {
		c.hub.setReadMark(id.UserID, req.Channel, req.Message, false)
	}
}

// setReadMark stores a registered user's read mark and tells the user's
// web sessions, so their unread marks follow (the sender too: else its
// marks stay behind, and it sends them again on every event). back: the
// mark may go back (mark unread); the sessions take it as it is.
func (h *Hub) setReadMark(user uint32, channel, message int64, back bool) {
	set := h.store.SetReadMark
	if back {
		set = h.store.ResetReadMark
	}
	if err := set(user, channel, message); err != nil {
		slog.Error("read mark", "err", err)
		return
	}
	msg := map[string]any{"type": "read", "channel": channel, "message": message}
	if back {
		msg["back"] = true
	}
	h.eachReady(func(c *Conn, id mumble.Identity) {
		if id.Registered && id.UserID == user {
			c.send(msg)
		}
	})
}
