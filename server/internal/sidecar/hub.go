// Package sidecar serves the Kallisti features that Mumble does not have
// (text channels with history) over JSON text frames on the bridge
// WebSocket.
package sidecar

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/time/rate"

	"kallisti.chat/server/internal/emoji"
	"kallisti.chat/server/internal/homes"
	"kallisti.chat/server/internal/msgrules"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/push"
	"kallisti.chat/server/internal/store"
)

const (
	MaxTextLength = 2000 // characters, like Discord
	maxNonce      = 64   // bytes of a send's nonce
	// Small pages: a page goes out as one frame on the voice connection.
	pageSize   = 50
	outboxSize = 64
	// Requests per connection: a steady rate with a short burst.
	perSecond = 5
	burst     = 10
	// An upload token is valid this long, for one upload.
	tokenLifetime = 5 * time.Minute
)

// Voice is the built-in Mumble server, for voice channel chats: its
// permissions, and delivery to Mumble clients.
type Voice interface {
	MayRead(session, channel uint32) bool
	MayWrite(session, channel uint32) bool
	SendChannelText(from, channel uint32, text string)
	// MayManage: the user may create, rename, order and delete text
	// channels.
	MayManage(session uint32) bool
	// MessagesOf: whose messages the actor may delete at once: a user by
	// session (0: a registered user by id); registered users come back by
	// user id, guests by session.
	MessagesOf(actor, session, user uint32) (registered bool, id uint32, err error)
	// SetModerator makes a registered user a moderator, or not (admins).
	SetModerator(session, user uint32, on bool) error
	// SetAdmin makes a registered user an admin (admins), or not
	// (SuperUser).
	SetAdmin(session, user uint32, on bool) error
	// Roles: registered user ids of admins and moderators.
	Roles() (admins, moderators []uint32)
	// ModeratesIn: the user is staff or a moderator of the Mumble channel
	// (or one above it). ChannelModerators lists each channel's own
	// moderators; SetChannelModerator makes a registered user one, or not
	// (admins and moderators).
	ModeratesIn(session, channel uint32) bool
	ChannelModerators() []ChannelModerators
	SetChannelModerator(session, user, channel uint32, on bool) error
	// SetListenOnly makes a voice channel listen-only for all but the
	// given registered users, or not (admins and moderators).
	SetListenOnly(session, channel uint32, on bool, speakers []uint32) error
	// ListenOnly lists the listen-only voice channels (speaker lists are
	// never nil).
	ListenOnly() []ListenOnlyChannel
	// SyncTextTree keeps a Mumble channel per category and per text
	// channel (in its category's, in order) and returns them.
	SyncTextTree(cats []MirrorCategory, want []Mirror) (catIDs, ids []uint32, err error)
	// Sees: the user may see a Mumble channel (others are hidden from it;
	// a private text channel is its mirror's access). Private lists the
	// private channels; SetPrivate makes one private with these members
	// (registered user ids), or public; CreatePrivateChannel makes a
	// private voice channel (below parent; channel moderators in their
	// channels). Only admins and moderators change them.
	Sees(session, channel uint32) bool
	// UserSees and UserMayRead are Sees and MayRead for a registered user
	// by id, also when it is not connected (push notifications).
	UserSees(user, channel uint32) bool
	UserMayRead(user, channel uint32) bool
	// UserBanned: a registered user is banned (it gets no pushes).
	UserBanned(user uint32) bool
	Private() []PrivateChannel
	// InPrivate: the user is a member of a private channel (also by its
	// category), not counting staff.
	InPrivate(session, channel uint32) bool
	SetPrivate(session, channel uint32, on bool, members []uint32) error
	CreatePrivateChannel(session, parent uint32, name string) (uint32, error)
	// AddMember adds a registered user to a private channel's members (by
	// an invite link, see invites.go); false: it already was.
	AddMember(channel, user uint32) (bool, error)
	// SendServerText sends HTML to one Mumble user, from the server.
	SendServerText(session uint32, html string)
	// SendPrivateText sends plain text to a registered user's Mumble
	// clients as a private message from a session (named in the text if
	// it is gone); false: nobody got it.
	SendPrivateText(from uint32, fromName string, toUser uint32, text string) bool
	// UserNames: the names of registered users (unknown ids are left out).
	UserNames(ids []uint32) map[uint32]string
	// Users: all registered users' names but SuperUser's, by id. Bio: a
	// registered user's saved comment ("": none). Pictures: the registered
	// users with a saved picture (at /api/avatars/<name>).
	Users() map[uint32]string
	Bio(user uint32) string
	Pictures() map[uint32]bool
	// Online: the connected registered users but SuperUser, by id, in any
	// channel.
	Online() map[uint32]OnlineUser
	// MumbleClient is who the session is (also a web session's), and
	// whether it is a Mumble client (not a web client).
	MumbleClient(session uint32) (MumbleUser, bool)
	// Ban bans a user for a time (0: forever), if the actor may.
	Ban(actor, session uint32, reason string, d time.Duration) error
	// SetTimeout times a user out for a time, or ends it (0), if the
	// actor may; TimedOut: the session's user is timed out, until when;
	// Timeouts: the running time-outs.
	SetTimeout(actor, session uint32, d time.Duration) error
	TimedOut(session uint32) (time.Time, bool)
	Timeouts() []Timeout
	// The same for channel time-outs: in a Mumble channel and below it
	// (staff and the channel's moderators give them).
	SetChannelTimeout(actor, session, channel uint32, d time.Duration) error
	ChannelTimedOut(session, channel uint32) (time.Time, bool)
	ChannelTimeouts() []Timeout
	// SetGuestsReadOnly: guests may only read, or write again (admins).
	// ReadOnly: the session's user is a guest that may not write now.
	SetGuestsReadOnly(session uint32, on bool) error
	GuestsReadOnly() bool
	ReadOnly(session uint32) bool
	// SetCaptcha: the level of the browser check for new accounts
	// (admins); Captcha: the level now.
	SetCaptcha(session uint32, level string) error
	Captcha() string
	// SetServerName: admins rename the server (the root channel: clients
	// see it as a Mumble channel rename).
	SetServerName(session uint32, name string) (string, error)
	// IsAdmin: the session's user is an admin (the logo, see logo.go).
	IsAdmin(session uint32) bool
	// Connections: every connection over the recent window (admins).
	Connections(session uint32) ([]Connection, error)
	// CheckMessage: a session's message to post (with the word
	// replacements), or why the message rules refuse it. MessageRules,
	// SetMessageRules: the rules (admins).
	CheckMessage(session uint32, m msgrules.Message) (string, error)
	MessageRules(session uint32) (msgrules.Rules, error)
	SetMessageRules(session uint32, r msgrules.Rules) (msgrules.Rules, error)
	// HomeServers, SetHomeServers: which other servers' accounts log in
	// here (admins).
	HomeServers(session uint32) (homes.Rules, error)
	SetHomeServers(session uint32, r homes.Rules) (homes.Rules, error)
}

// LiveStream is a screen share or camera in a voice channel, by its id
// (the streamer's session; a camera's has stream.CameraBit set too).
type LiveStream struct {
	Session uint32 `json:"session"`
	Channel uint32 `json:"channel"`
	Preview string `json:"preview,omitempty"` // a JPEG data URL; for those who may enter the channel
}

// StreamsChanged sends everyone the live streams in channels it sees
// (previews come with StreamPreview).
func (h *Hub) StreamsChanged(streams []LiveStream) {
	if h.voice == nil {
		return
	}
	h.eachReady(func(c *Conn, id mumble.Identity) {
		out := []LiveStream{}
		for _, s := range streams {
			if h.voice.Sees(id.Session, s.Channel) {
				out = append(out, LiveStream{Session: s.Session, Channel: s.Channel})
			}
		}
		c.send(map[string]any{"type": "streams", "streams": out})
	})
}

// StreamPreview sends a stream's new thumbnail to those who may enter its
// channel.
func (h *Hub) StreamPreview(s LiveStream) {
	h.sendTo(chat{voice: s.Channel, isVoice: true}, map[string]any{"type": "stream-preview", "session": s.Session, "image": s.Preview})
}

// Notice tells one voice session's connection something (a problem of its
// stream from OBS); the web app shows it for a moment.
func (h *Hub) Notice(session uint32, text string) {
	h.eachReady(func(c *Conn, id mumble.Identity) {
		if id.Session == session {
			c.send(map[string]any{"type": "notice", "message": text})
		}
	})
}

// liveStreams for one user: previews only of channels it may enter.
func (h *Hub) liveStreams(id mumble.Identity) []LiveStream {
	out := []LiveStream{}
	for _, s := range h.LiveStreams() {
		if !h.voice.Sees(id.Session, s.Channel) {
			continue
		}
		if !h.voice.MayRead(id.Session, s.Channel) {
			s.Preview = ""
		}
		out = append(out, s)
	}
	return out
}

// ListenOnlyChannel is a voice channel where only its speakers may speak.
type ListenOnlyChannel struct {
	Channel  uint32    `json:"channel"`
	Speakers []Speaker `json:"speakers"`
}

// Speaker is a registered user who may speak in a listen-only channel.
type Speaker struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

// OnlineUser is a connected registered user (see Voice.Online).
type OnlineUser struct {
	Name    string
	Session uint32
}

// MumbleUser is who a Mumble client is (see Voice.MumbleClient).
type MumbleUser struct {
	Registered bool
	UserID     uint32 // with Registered
}

type Hub struct {
	store *store.Store
	now   func() time.Time
	// Posts per webhook (see webhookpost.go).
	webhookLimits webhookLimits
	// Nil: no voice channel chats (a Murmur server).
	voice Voice
	// The server's public address, for upload links sent to Mumble clients.
	publicBase string
	// Text channels and their categories; changed by admins (with the
	// built-in server). Under mu.
	channels   []wireChannel
	categories []wireCategory
	mu         sync.Mutex
	// One channel change at a time: store, reload and broadcast in order.
	manageMu sync.Mutex
	// Guest messages of this run: message id -> voice server session (see
	// wrote).
	guestMessages map[int64]uint32
	// UploadMax: the size limit per upload, for clients to check first.
	UploadMax int64
	// OnUploadDeleted removes the file of a deleted message's attachment.
	OnUploadDeleted func(key string)
	// Avatars: online users' pictures are at /api/avatars/<name> too, so
	// clients need not fetch them on the voice connection.
	Avatars bool
	// Previews makes link previews (nil: none).
	Previews Previewer
	// Screen shares (built-in server): a token for the stream connection,
	// and the live streams.
	StreamToken func(session uint32) string
	LiveStreams func() []LiveStream
	// Channel recordings (built-in server; nil: none).
	Recorder Recorder
	// SaveFile stores a picture from a Mumble message as an upload.
	SaveFile func(author, name, ctype string, data []byte) (store.Upload, error)
	// OnHomesChanged runs after admins changed which home servers' accounts
	// log in here (nil: nothing).
	OnHomesChanged func()
	// Push sends a push notification to a user's browsers, by name (nil:
	// none; see push.go). It must not block.
	Push func(name string, n push.Note)
	// Per voice server session: the last message seen in each mirror.
	visitsMu sync.Mutex
	visits   map[uint32]*visit
	conns    map[*Conn]struct{}
	tokens   map[string]uploadToken
	// One status change at a time, from the change to its broadcast (taken
	// before mu and Conn.mu; see status.go). Also guards online: the
	// members online as last told, with their sessions (see members.go).
	statusMu sync.Mutex
	online   map[uint32]uint32
	// Custom emoji (see emoji.go). Under mu; emojiMu: one reload at a time
	// (also of personal emoji lists, see useremoji.go).
	emoji   []wireEmoji
	emojiMu sync.Mutex
	// The stickers (see stickers.go), under mu; stickersMu: one change at
	// a time.
	stickers   []wireSticker
	stickersMu sync.Mutex
	slow       slowPosts // see slowmode.go
}

// emoji: a token for adding this custom emoji (not for a file); personal:
// to the user's own emoji (see useremoji.go).
type uploadToken struct {
	author   string
	emoji    string
	user     uint32
	personal bool
	sticker  *store.Sticker // a token for adding this sticker
	logo     bool           // for the server's logo
	webhook  int64          // for this webhook's picture
	expires  time.Time
}

func NewHub(s *store.Store, now func() time.Time) (*Hub, error) {
	h := &Hub{store: s, now: now, conns: map[*Conn]struct{}{}, tokens: map[string]uploadToken{}}
	if err := h.reloadChannels(); err != nil {
		return nil, err
	}
	if err := h.loadStickers(); err != nil {
		return h, err
	}
	return h, h.loadEmoji()
}

// SetVoice turns on stored voice channel chats. Call it before clients
// connect.
func (h *Hub) SetVoice(v Voice, publicBase string) {
	h.voice, h.publicBase = v, strings.TrimSuffix(publicBase, "/")
	h.manageMu.Lock()
	defer h.manageMu.Unlock()
	if err := h.syncMirrors(0); err != nil {
		slog.Error("text channel mirrors", "err", err)
	}
}

func (h *Hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// Conn is the sidecar side of one bridge connection. Outgoing messages go
// to a bounded outbox; the bridge writes them to the WebSocket.
type Conn struct {
	hub      *Hub
	out      chan []byte
	limit    *rate.Limiter
	limitLog time.Time // the last "over the limit" log line (one a minute)
	mu       sync.Mutex
	identity *mumble.Identity
	ready    bool
	closed   bool
	mic      *wireMic // as the client's browser says (see connections.go)
	// Messages were dropped (full outbox): the client must reload.
	lagged  bool
	dropped uint64 // messages lost so far (see Overflows)
	// The last typing signal passed on, per channel (see typing).
	typed map[int64]time.Time
	// The chosen status (kept for registered users) and no input for a
	// while (see status.go).
	status store.UserStatus
	away   bool
	sent   statusView // the status others were told (see syncStatuses)
	// Ends with the connection (work on its behalf stops); previewing:
	// previews on their way.
	ctx        context.Context
	stop       context.CancelFunc
	previewing int
	// The nonce of the send being handled: every error it answers with
	// carries it, so the client fails only that send (see Handle).
	answering string
}

func (h *Hub) Connect() *Conn {
	ctx, stop := context.WithCancel(context.Background())
	return &Conn{hub: h, out: make(chan []byte, outboxSize), limit: rate.NewLimiter(perSecond, burst), ctx: ctx, stop: stop}
}

// Outbox is closed when the connection closes.
func (c *Conn) Outbox() <-chan []byte { return c.out }

func (c *Conn) Close() {
	c.stop()
	c.mu.Lock()
	id := c.identity
	c.mu.Unlock()
	c.hub.mu.Lock()
	delete(c.hub.conns, c)
	// Murmur reuses session numbers: the next guest owns none of these.
	if id != nil && !id.Registered {
		for m, session := range c.hub.guestMessages {
			if session == id.Session {
				delete(c.hub.guestMessages, m)
			}
		}
	}
	c.hub.mu.Unlock()
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.out)
	}
	c.mu.Unlock()
	c.statusGone()
}

// SetIdentity is called when the Mumble identity is known or changes. The
// first call makes the connection ready.
func (c *Conn) SetIdentity(id mumble.Identity) {
	// Same lock order as Close (hub, then conn): a connection that closed
	// meanwhile is never registered. No name yet: not ready.
	c.hub.mu.Lock()
	c.mu.Lock()
	becomesReady := !c.ready && !c.closed && id.Name != ""
	c.identity = &id
	if becomesReady {
		c.ready = true
		c.hub.conns[c] = struct{}{}
	}
	wasReady := c.ready && !becomesReady
	c.mu.Unlock()
	c.hub.mu.Unlock()
	if wasReady && c.hub.voice != nil {
		c.sendAccess(id) // a registration can change what it sees
	}
	if becomesReady {
		c.sendReady(id, c.hub.readyFor(id))
	}
}

// Fail tells the client that text channels do not work on this connection.
// It comes from the bridge's other goroutine: not a map, so it never
// carries the nonce of a send being handled (see send).
func (c *Conn) Fail(reason string) {
	c.send(struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}{"error", reason})
}

type wireChannel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// The Mumble channel that mirrors it (the web client hides it from
	// the voice channels).
	Mumble uint32 `json:"mumble,omitempty"`
	Topic  string `json:"topic,omitempty"`
	// Seconds between a user's posts (built-in server; see slowmode.go).
	SlowMode int64 `json:"slowMode,omitempty"`
	Category int64 `json:"category,omitempty"` // see categories.go
}

type wireMessage struct {
	ID         int64           `json:"id"`
	Channel    int64           `json:"channel"`
	Author     string          `json:"author"`
	Text       string          `json:"text"`
	At         int64           `json:"at"` // Unix milliseconds
	Verified   bool            `json:"verified"`
	AuthorID   *uint32         `json:"authorId,omitempty"` // with verified (unknown for old messages)
	Uploads    []wireUpload    `json:"uploads,omitempty"`
	Upload     *wireUpload     `json:"upload,omitempty"`  // the first of uploads, for older clients
	Sticker    *wireMsgSticker `json:"sticker,omitempty"` // a sticker message (text: its name, for clients without stickers)
	Edited     int64           `json:"edited,omitempty"`  // Unix milliseconds
	Pinned     int64           `json:"pinned,omitempty"`  // Unix milliseconds
	Reply      *wireQuote      `json:"reply,omitempty"`
	Reactions  []wireReaction  `json:"reactions,omitempty"`
	MentionAll string          `json:"mentionAll,omitempty"` // "here" or "everyone"
	NoPing     bool            `json:"noPing,omitempty"`     // a reply that does not ping
	Webhook    int64           `json:"webhook,omitempty"`    // posted by this webhook (see webhooks.go)
	Thread     *wireThread     `json:"thread,omitempty"`     // the thread started from it
	Poll       *wirePoll       `json:"poll,omitempty"`       // a poll message (text: the poll as text)
}

// wireMsgSticker is the sticker a message is: its id and name (a client
// shows the name when the id is not in its sticker list: deleted).
type wireMsgSticker struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// wireQuote is the start of the message a reply answers (author empty:
// deleted since).
type wireQuote struct {
	ID      int64  `json:"id"`
	Author  string `json:"author"`
	Text    string `json:"text"`
	Webhook bool   `json:"webhook,omitempty"` // a webhook's post (see store.Quote)
}

const quoteLength = 100

func shorten(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	return string([]rune(text)[:n]) + "…"
}

type wireUpload struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	// Pixels, milliseconds: unknown if missing.
	Width    int   `json:"width,omitempty"`
	Height   int   `json:"height,omitempty"`
	Duration int64 `json:"duration,omitempty"`
	// A smaller picture (see files' previews): a video's poster, a large
	// picture for the chat, a large GIF's video (previewType video/mp4,
	// shown like a picture).
	Preview     string `json:"preview,omitempty"`
	PreviewType string `json:"previewType,omitempty"`
	Description string `json:"description,omitempty"` // alt text
}

// wireAttachment is an upload to attach to a message being sent.
// A spoiler's name starts with "SPOILER_" (the clients' choice).
type wireAttachment struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

// FileURL is where the server serves an upload (relative to the server).
func FileURL(u store.Upload) string {
	return "/files/" + u.Key + "/" + url.PathEscape(u.Name)
}

// previewURL is where its preview is ("": none).
func previewURL(u store.Upload) string {
	if !u.HasPreview() {
		return ""
	}
	return FileURL(u) + "?preview"
}

// previewType is its preview's type ("": none).
func previewType(u store.Upload) string {
	if !u.HasPreview() {
		return ""
	}
	return u.Preview
}

// UploadPreview: an upload got its preview. If it is attached already,
// its message goes out again with it (as an edit, but not marked edited);
// otherwise the message will carry it when sent.
func (h *Hub) UploadPreview(key string) {
	m, err := h.store.MessageByUpload(key)
	if err != nil {
		return
	}
	if ch, ok := h.chatOf(m.ChannelID); ok {
		h.sendTo(ch, map[string]any{"type": "message-edited", "message": toWire(m)})
	}
}

func toWire(m store.Message) wireMessage { return toWireFor(m, nil) }

// toWireFor: for one reader (see toWirePoll).
func toWireFor(m store.Message, viewer *uint32) wireMessage {
	w := wireMessage{ID: m.ID, Channel: m.ChannelID, Author: m.Author, Text: m.Text, At: m.CreatedAt.UnixMilli(), Verified: m.Verified, AuthorID: m.AuthorID, Webhook: m.Webhook}
	if !m.EditedAt.IsZero() {
		w.Edited = m.EditedAt.UnixMilli()
	}
	if !m.PinnedAt.IsZero() {
		w.Pinned = m.PinnedAt.UnixMilli()
	}
	if r := m.Reply; r != nil {
		w.Reply = &wireQuote{ID: r.ID, Author: r.Author, Text: shorten(r.Text, quoteLength), Webhook: r.Webhook}
	}
	for _, u := range m.Uploads {
		w.Uploads = append(w.Uploads, wireUpload{URL: FileURL(u), Name: u.Name, Type: u.Type, Size: u.Size, Width: u.Width, Height: u.Height,
			Duration: u.Duration, Preview: previewURL(u), PreviewType: previewType(u), Description: u.Description})
	}
	if len(w.Uploads) > 0 {
		w.Upload = &w.Uploads[0]
	}
	if s := m.Sticker; s != nil {
		w.Sticker = &wireMsgSticker{ID: s.ID, Name: s.Name}
	}
	if m.Poll != nil {
		w.Poll = toWirePoll(*m.Poll, viewer)
	}
	if len(m.Reactions) > 0 {
		w.Reactions = toWireReactions(m.Reactions)
	}
	w.MentionAll, w.NoPing = m.MentionAll, m.NoPing
	if m.Thread != nil {
		t := toWireThread(*m.Thread)
		t.Quote = nil // this message
		w.Thread = &t
	}
	return w
}

type request struct {
	Type        string           `json:"type"`
	Name        string           `json:"name"`       // create-channel, rename-channel; emoji-token, delete-emoji; the user-emoji requests
	To          string           `json:"to"`         // rename-user-emoji: the new name
	Channels    []int64          `json:"channels"`   // order-channels
	Category    int64            `json:"category"`   // create-channel, set-category, rename-category, delete-category
	Cats        []int64          `json:"categories"` // order-categories
	Voice       uint32           `json:"voice"`      // voice-chat, start-recording, stop-recording: the Mumble channel; create-private-voice: the parent
	Channel     int64            `json:"channel"`
	Before      int64            `json:"before"`
	Text        string           `json:"text"`
	Uploads     []wireAttachment `json:"uploads"` // send: the uploads to attach, in order
	Count       int              `json:"count"`   // upload-token: how many (the files of one message)
	Remove      []string         `json:"remove"`  // edit-message: the keys of files to take off
	Poll        *wirePollRequest `json:"poll"`    // send: a poll message
	Answers     []int            `json:"answers"` // vote: the answers (none: the vote taken back)
	Mic         *wireMic         `json:"mic"`     // mic-info
	poll        *store.Poll      // send: the checked Poll
	Upload      string           `json:"upload"`      // send: one upload's key (older clients)
	Message     int64            `json:"message"`     // edit-message, delete-message, create-thread
	Reply       int64            `json:"reply"`       // send: the message it answers
	NoPing      bool             `json:"noPing"`      // send: the reply does not ping
	Nonce       string           `json:"nonce"`       // send: the client's id for it, the same on a retry (see store.MessageByNonce)
	Author      string           `json:"author"`      // search: only this author
	User        uint32           `json:"user"`        // set-moderator, set-admin, dm, bio, delete-messages: registered user id
	Moderator   bool             `json:"moderator"`   // set-moderator
	Admin       bool             `json:"admin"`       // set-admin
	Users       []uint32         `json:"users"`       // set-listen-only: who may speak
	Messages    []int64          `json:"messages"`    // previews
	Emoji       string           `json:"emoji"`       // react; add-user-emoji, remove-emoji-image; sticker-token, rename-sticker: the related emoji
	Sticker     int64            `json:"sticker"`     // send: a sticker message; rename-sticker, delete-sticker
	Description string           `json:"description"` // sticker-token, rename-sticker
	Pack        string           `json:"pack"`        // sticker-token, rename-sticker
	On          bool             `json:"on"`          // react: add (or remove); pin; away; set-guests-read-only
	Status      string           `json:"status"`      // set-status: "online", "idle" or "dnd"
	Level       string           `json:"level"`       // set-captcha
	Until       int64            `json:"until"`       // set-status: end of the custom status (Unix ms)
	Session     uint32           `json:"session"`     // ban, time-out, delete-messages: the user's voice server session
	Seconds     int64            `json:"seconds"`     // ban (0: forever), time-out (0: end it), delete-messages (the last seconds), set-slow-mode (0: off)
	Private     bool             `json:"private"`     // create-channel, create-category: a private one
	List        *channelList     `json:"list"`        // set-channel-list
	Uses        int              `json:"uses"`        // create-invite (0: unlimited; seconds 0: for ever)
	Invite      int64            `json:"invite"`      // delete-invite
	Webhook     int64            `json:"webhook"`     // rename-webhook, delete-webhook
	Token       string           `json:"token"`       // redeem-invite
	Rules       *msgrules.Rules  `json:"rules"`       // set-message-rules
	Homes       *homes.Rules     `json:"homes"`       // set-home-servers
}

// Handle answers one request from the browser.
// overLimit answers a request over the rate limit, marked so the client
// does not show it as a lasting error (a send comes back for the composer),
// and logs which requests do it, once a minute per connection.
// composerText goes back to the composer with a refused send (a poll
// stays in its form).
func (r request) composerText() string {
	if r.poll != nil {
		return ""
	}
	return r.Text
}

func (c *Conn) overLimit(id mumble.Identity, req request) {
	const text = "too many requests, slow down"
	c.mu.Lock()
	logIt := time.Since(c.limitLog) > time.Minute
	if logIt {
		c.limitLog = time.Now()
	}
	c.mu.Unlock()
	if logIt {
		slog.Warn("requests over the limit", "user", id.Name, "session", id.Session, "type", req.Type)
	}
	if req.Type == "send" {
		c.send(map[string]any{"type": "error", "message": text, "about": "refused", "channel": req.Channel, "text": req.composerText()})
		return
	}
	c.send(map[string]any{"type": "error", "message": text, "about": "rate"})
}

func (c *Conn) Handle(raw []byte) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		c.sendError("invalid request")
		return
	}
	if req.Upload != "" && req.Uploads == nil {
		req.Uploads = []wireAttachment{{Key: req.Upload}}
	}
	// Requests come one at a time (one reader per connection).
	if req.Type == "send" && len(req.Nonce) <= maxNonce {
		c.setAnswering(req.Nonce)
		defer c.setAnswering("")
	}
	// A poll's text goes through the rules as any text.
	if req.Type == "send" && req.Poll != nil {
		p, why := newPoll(*req.Poll, c.hub.now(), func(e string) (string, string) { return c.hub.usableEmoji(e, true) })
		if why != "" {
			c.sendError(why)
			return
		}
		req.poll, req.Text = &p, pollText(p)
	}
	c.mu.Lock()
	id, ready := c.identity, c.ready
	c.mu.Unlock()
	if !ready {
		c.sendError("not ready")
		return
	}
	if !c.limit.Allow() {
		c.overLimit(*id, req)
		return
	}
	if c.readOnly(*id, req) || c.refused(*id, &req) {
		return
	}
	if req.Type == "upload-token" {
		c.uploadTokens(*id, req.Count)
		return
	}
	if chatWrites[req.Type] && c.timedOut(*id, req) {
		return
	}
	if req.Type == "voice-chat" {
		c.voiceChat(*id, req.Voice)
		return
	}
	switch req.Type {
	case "create-channel", "rename-channel", "delete-channel", "order-channels", "set-topic", "set-slow-mode",
		"create-category", "rename-category", "delete-category", "order-categories", "set-category":
		c.manage(*id, req)
		return
	case "mark-read", "mark-unread":
		c.markRead(*id, req, req.Type == "mark-unread")
		return
	case "resync":
		c.resendReady(*id)
		return
	case "create-thread", "threads", "get-thread", "rename-thread", "archive-thread", "delete-thread", "follow-thread", "set-thread-archive":
		c.threadRequest(*id, req)
		return
	case "search":
		c.search(*id, req)
		return
	case "edit-message", "delete-message":
		c.changeMessage(*id, req)
		return
	case "delete-messages":
		c.deleteMessagesOf(*id, req)
		return
	case "react":
		c.react(*id, req)
		return
	case "vote", "end-poll":
		c.pollAction(*id, req)
		return
	case "mic-info":
		c.setMic(req.Mic)
		return
	case "pin":
		c.pin(*id, req)
		return
	case "previews":
		c.previews(*id, req)
		return
	case "set-moderator", "set-admin":
		// The voice server reports the change (RolesChanged).
		if c.hub.voice == nil {
			c.manageError("roles need the built-in Mumble server")
			return
		}
		set, on := c.hub.voice.SetModerator, req.Moderator
		if req.Type == "set-admin" {
			set, on = c.hub.voice.SetAdmin, req.Admin
		}
		if err := set(id.Session, req.User, on); err != nil {
			c.manageError(err.Error())
		}
		return
	case "ban":
		c.ban(*id, req)
		return
	case "time-out":
		c.timeOut(*id, req)
		return
	case "channel-time-out":
		c.channelTimeOut(*id, req)
		return
	case "set-channel-moderator":
		c.setChannelModerator(*id, req)
		return
	case "dm":
		c.direct(*id, req.User)
		return
	case "bio":
		c.bio(req.User)
		return
	case "set-status":
		c.setStatus(*id, req)
		return
	case "set-channel-list":
		c.setChannelList(*id, req.List)
		return
	case "away":
		c.setAway(*id, req.On)
		return
	case "emoji-token":
		c.emojiToken(*id, req.Name)
		return
	case "delete-emoji":
		c.deleteEmoji(*id, req.Name)
		return
	case "sticker-token", "rename-sticker", "delete-sticker":
		c.stickerRequest(*id, req)
		return
	case "user-emoji-token", "add-user-emoji", "rename-user-emoji", "delete-user-emoji", "remove-emoji-image":
		c.userEmoji(*id, req)
		return
	case "stream-token":
		if c.hub.StreamToken == nil {
			c.sendError("screen sharing needs the built-in Mumble server")
		} else {
			c.send(map[string]any{"type": "stream-token", "token": c.hub.StreamToken(id.Session)})
		}
		return
	case "set-private":
		c.setPrivate(*id, req)
		return
	case "start-recording", "stop-recording", "recordings", "recording-token":
		c.recordings(*id, req)
		return
	case "create-private-voice":
		c.createPrivateVoice(*id, req.Voice, req.Name)
		return
	case "invites":
		c.invites(*id, req)
		return
	case "set-invite-only", "registration-invites", "create-registration-invite", "delete-registration-invite":
		c.registrationRequest(*id, req)
		return
	case "create-invite":
		c.createInvite(*id, req)
		return
	case "delete-invite":
		c.deleteInvite(*id, req)
		return
	case "redeem-invite":
		c.redeemInvite(*id, req.Token)
		return
	case "webhooks", "create-webhook", "rename-webhook", "delete-webhook", "webhook-picture-token", "remove-webhook-picture":
		c.webhookRequest(*id, req)
		return
	case "set-guests-read-only":
		// The voice server reports the change (GuestsReadOnlyChanged).
		if c.hub.voice == nil {
			c.manageError("this setting needs the built-in Mumble server")
		} else if err := c.hub.voice.SetGuestsReadOnly(id.Session, req.On); err != nil {
			c.manageError(err.Error())
		}
		return
	case "set-captcha":
		if c.hub.voice == nil {
			c.manageError("this setting needs the built-in Mumble server")
		} else if err := c.hub.voice.SetCaptcha(id.Session, req.Level); err != nil {
			c.manageError(err.Error())
		} else {
			c.hub.broadcast(map[string]any{"type": "captcha", "level": req.Level})
		}
		return
	case "set-server-name":
		if c.hub.voice == nil {
			c.manageError("this setting needs the built-in Mumble server")
		} else if name, err := c.hub.voice.SetServerName(id.Session, req.Name); err != nil {
			c.manageError(err.Error())
		} else {
			c.hub.broadcast(map[string]any{"type": "server-name", "name": name})
		}
		return
	case "logo-token":
		c.logoToken(*id)
		return
	case "remove-logo":
		c.removeLogo(*id)
		return
	case "connections":
		c.connections(id.Session)
		return
	case "message-rules", "set-message-rules":
		c.messageRules(*id, req)
		return
	case "home-servers", "set-home-servers":
		c.homeServers(*id, req)
		return
	case "set-listen-only":
		// The voice server reports the change (ListenOnlyChanged).
		if c.hub.voice == nil {
			c.manageError("listen-only channels need the built-in Mumble server")
		} else if err := c.hub.voice.SetListenOnly(id.Session, req.Voice, req.On, req.Users); err != nil {
			c.manageError(err.Error())
		}
		return
	}
	if req.Type != "send" && req.Type != "history" && req.Type != "typing" && req.Type != "pins" {
		c.sendError("unknown request type")
		return
	}
	ch, ok := c.hub.chatOf(req.Channel)
	if !ok {
		if req.Type != "typing" { // a channel deleted while typing
			c.sendError("unknown channel")
		}
		return
	}
	if !c.hub.may(*id, ch, req.Type != "history" && req.Type != "pins") {
		if req.Type != "typing" { // only typing: no error to show
			c.sendError("you may not use this channel's chat")
		}
		return
	}
	switch req.Type {
	case "history":
		c.history(req)
	case "pins":
		c.pins(req)
	case "send":
		c.post(*id, req, ch)
	case "typing":
		c.typing(*id, ch)
	}
}

// voiceChat tells the client which channel holds a voice channel's chat.
func (c *Conn) voiceChat(id mumble.Identity, voice uint32) {
	if c.hub.voice == nil {
		c.sendError("voice channel chats need the built-in Mumble server")
		return
	}
	if !c.hub.voice.MayRead(id.Session, voice) {
		c.sendError("you may not use this channel's chat")
		return
	}
	channel, err := c.hub.store.VoiceChat(voice)
	if err != nil {
		c.sendError("cannot open the chat")
		return
	}
	c.send(map[string]any{"type": "voice-chat", "voice": voice, "channel": channel})
}

// MumbleText stores a text a Mumble client sent to a channel, and shows it
// to the web clients that may read it (see mumbled.Config.OnChannelText).
// In a text channel's mirror, it goes to the text channel (all web users).
// Images become attachments: the first on the text, others on messages of
// their own.
func (h *Hub) MumbleText(from mumble.Identity, channel uint32, text string, images []Image) {
	if utf8.RuneCountInString(text) > MaxTextLength {
		text = string([]rune(text)[:MaxTextLength])
	}
	var where chat
	if c, ok := h.textOf(channel); ok {
		where = chat{id: c.ID}
	} else {
		id, err := h.store.VoiceChat(channel)
		if err != nil {
			slog.Error("voice channel chat", "channel", channel, "err", err)
			return
		}
		where = chat{id: id, voice: channel, isVoice: true}
	}
	h.storeMumble(from, where, text, images)
}

// storeMumble stores a message from a Mumble client in a chat and sends it
// to the chat's readers. It returns how many messages it stored and the
// last one's id.
func (h *Hub) storeMumble(from mumble.Identity, where chat, text string, images []Image) (n int, last int64) {
	store1 := func(text string, uploads []store.Attachment) {
		m, err := h.store.AddMessage(store.NewMessage{ChannelID: where.id, Author: from.Name, Verified: from.Registered, AuthorID: from.UserID, Text: text, At: h.now(), Uploads: uploads})
		if err != nil {
			slog.Error("store a Mumble message", "err", err)
			return
		}
		n, last = n+1, m.ID
		h.sendTo(where, map[string]any{"type": "message", "message": toWire(m)})
		h.pushFor(where, m)
	}
	// Only registered users' images are kept (with their upload quota):
	// guests can use any name. Mumble clients see them live anyway.
	var uploads []store.Attachment
	if h.SaveFile != nil && from.Registered {
		for i, img := range images {
			u, err := h.SaveFile(from.Name, fmt.Sprintf("image-%d%s", i+1, imageExt(img.Type)), img.Type, img.Data)
			if err != nil {
				slog.Error("store a Mumble image", "err", err)
				continue
			}
			uploads = append(uploads, store.Attachment{ID: u.ID})
		}
	}
	// One message with the text and the images; more than a message takes
	// go in further ones. If storing fails, the uploads have no message:
	// the cleanup of unused uploads removes them.
	for len(uploads) > 0 || text != "" {
		batch := uploads[:min(len(uploads), store.MaxUploads)]
		store1(text, batch)
		text, uploads = "", uploads[len(batch):]
	}
	return n, last
}

// Image is a picture from a Mumble message (see mumbled.Image).
type Image struct {
	Type string
	Data []byte
}

func imageExt(typ string) string {
	switch typ {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ".jpg"
}

func (h *Hub) channelExists(id int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.channels {
		if ch.ID == id {
			return true
		}
	}
	return false
}

func (c *Conn) history(req request) {
	// One extra row tells whether there are older messages.
	msgs, err := c.hub.store.History(req.Channel, req.Before, pageSize+1)
	if err != nil {
		c.sendError("cannot load history")
		return
	}
	more := len(msgs) > pageSize
	if more {
		msgs = msgs[1:]
	}
	wire := make([]wireMessage, len(msgs))
	for i, m := range msgs {
		wire[i] = toWireFor(m, c.viewer())
	}
	c.send(map[string]any{"type": "history", "channel": req.Channel, "messages": wire, "more": more})
}

func (c *Conn) post(id mumble.Identity, req request, ch chat) {
	if len(req.Nonce) > maxNonce {
		c.sendError("invalid nonce")
		return
	}
	n := store.NewMessage{ChannelID: req.Channel, Thread: ch.parent != 0, Author: id.Name, Verified: id.Registered, AuthorID: id.UserID, At: c.hub.now(), StickerID: req.Sticker, ReplyTo: req.Reply, NoPing: req.NoPing && req.Reply != 0, Nonce: req.Nonce}
	// A retry of a stored send: its message again, to the sender only.
	stored := func() bool {
		m, err := c.hub.store.MessageByNonce(n)
		if err == nil {
			c.send(map[string]any{"type": "message", "message": toWire(m), "nonce": req.Nonce})
		}
		return err == nil
	}
	if stored() {
		return
	}
	typed := req.composerText() // what goes back to the composer (slow mode)
	if req.poll != nil {
		switch {
		case !id.Registered:
			c.sendError("join with an account (or a guest account) to make a poll")
			return
		case req.Sticker != 0 || len(req.Uploads) > 0:
			c.sendError("a poll is sent on its own")
			return
		}
		n.Poll, n.NoPing = req.poll, n.ReplyTo != 0 // polls ping nobody
	}
	// A sticker is a message of its own; its text is its name, for what
	// only shows text (Mumble clients, previews, quotes, search).
	if req.Sticker != 0 {
		st, err := c.hub.store.StickerByID(req.Sticker)
		switch {
		case strings.TrimSpace(req.Text) != "" || len(req.Uploads) > 0:
			c.sendError("a sticker is sent on its own")
			return
		case err != nil || st.Deleted:
			c.sendError("no such sticker")
			return
		}
		req.Text = stickerText(st.Name)
	}
	text, ok := validText(req.Text, len(req.Uploads) > 0)
	if !ok {
		c.sendError(textError)
		return
	}
	n.Text = text
	// Not in direct chats: there every message counts already.
	if !ch.direct && c.hub.voice != nil && c.hub.voice.MayManage(id.Session) {
		n.MentionAll = mentionAllOf(text)
	}
	if req.Reply != 0 {
		if orig, err := c.hub.store.MessageByID(req.Reply); err != nil || orig.ChannelID != req.Channel {
			c.sendError("the message to answer is not in this channel")
			return
		}
	}
	if n.Uploads, ok = c.attachments(id, req.Uploads); !ok {
		return
	}
	if c.slowMode(id, ch, typed) {
		return
	}
	m, err := c.hub.store.AddMessage(n)
	if errors.Is(err, store.ErrNotFound) { // a thread or the sticker deleted meanwhile
		if req.Sticker != 0 {
			c.sendError("no such sticker")
		} else {
			c.sendError("unknown channel")
		}
		return
	}
	if err != nil {
		if !stored() { // else the same send from another connection won
			c.sendError("cannot store message")
		}
		return
	}
	if !id.Registered {
		c.hub.rememberGuest(m.ID, id.Session)
	}
	c.hub.deliver(id, ch, m, c, req.Nonce)
}

// deliver sends a stored message of `from` to the chat's readers (from's
// connection `by` gets it with the nonce: it confirms that send), the
// thread's followers, push, and Mumble clients.
func (h *Hub) deliver(from mumble.Identity, ch chat, m store.Message, by *Conn, nonce string) {
	if ch.direct {
		h.announceDirect(ch)
	}
	if ch.parent != 0 {
		h.threadFollowers(from, ch, m)
	}
	ev := map[string]any{"type": "message", "message": toWire(m)}
	mine := ev
	if nonce != "" {
		mine = map[string]any{"type": "message", "message": ev["message"], "nonce": nonce}
	}
	for _, conn := range h.readers(ch) {
		if conn == by {
			conn.send(mine)
		} else {
			conn.send(ev)
		}
	}
	if ch.direct {
		h.relayDirect(from, ch, m)
	}
	if ch.parent != 0 {
		h.threadPosted(ch)
	}
	h.pushFor(ch, m)
	if ch.isVoice {
		h.voice.SendChannelText(from.Session, ch.voice, h.mumbleLive(m))
	} else if mirror := h.mirrorOf(ch.id); mirror != 0 && h.voice != nil {
		h.voice.SendChannelText(from.Session, mirror, h.mumbleLive(m))
	}
}

// mumbleLive is a new message for Mumble users: a webhook's comes from the
// server, so its name goes first (see authorLabel).
func (h *Hub) mumbleLive(m store.Message) string {
	if m.Webhook != 0 {
		return authorLabel(m) + ": " + h.mumbleText(m)
	}
	return h.mumbleText(m)
}

// authorLabel is the author as Mumble users see it: a webhook's name is
// marked (it may be anyone's).
func authorLabel(m store.Message) string { return webhookLabel(m.Author, m.Webhook != 0) }

func webhookLabel(name string, webhook bool) string {
	if webhook {
		return name + " (webhook)"
	}
	return name
}

// attachments are a send's uploads for the store: the sender's own, each
// once (the store refuses used ones), at most store.MaxUploads (else an error goes to the sender).
func (c *Conn) attachments(id mumble.Identity, uploads []wireAttachment) ([]store.Attachment, bool) {
	if len(uploads) > store.MaxUploads {
		c.sendError(fmt.Sprintf("at most %d files per message", store.MaxUploads))
		return nil, false
	}
	var out []store.Attachment
	for _, a := range uploads {
		u, err := c.hub.store.UploadByKey(a.Key)
		if err != nil || u.Author != id.Name || slices.ContainsFunc(out, func(b store.Attachment) bool { return b.ID == u.ID }) {
			c.sendError("unknown upload")
			return nil, false
		}
		description := strings.TrimSpace(a.Description)
		if utf8.RuneCountInString(description) > store.MaxDescription {
			c.sendError(fmt.Sprintf("a file description has at most %d characters", store.MaxDescription))
			return nil, false
		}
		out = append(out, store.Attachment{ID: u.ID, Description: description})
	}
	return out, true
}

// mumbleText is a stored message as Mumble clients see it: an upload as a
// link (or its name, without a public address), personal emoji as
// ":name:".
func (h *Hub) mumbleText(m store.Message) string {
	text := emoji.RefsAsNames(m.Text)
	for _, u := range m.Uploads {
		file := "[" + u.Name + "]"
		if h.publicBase != "" {
			file = h.publicBase + FileURL(u)
		}
		text = strings.TrimSpace(text + " " + file)
	}
	// Mumble clients have no replies: a short quote first.
	if r := m.Reply; r != nil && r.Author != "" {
		text = "> " + webhookLabel(r.Author, r.Webhook) + ": " + shorten(emoji.RefsAsNames(r.Text), 60) + " — " + text
	}
	return text
}

// readers are the connections whose user may read a chat (a text
// channel: everyone).

// roles are the registered user ids of admins and moderators, and the
// moderators of the channels the user sees.
type roles struct {
	Type              string              `json:"type,omitempty"` // "roles" as an event of its own
	Admins            []uint32            `json:"admins"`
	Moderators        []uint32            `json:"moderators"`
	ChannelModerators []ChannelModerators `json:"channelModerators"`
}

// roles for everyone; channel moderators come with forUser.
func (h *Hub) roles() roles {
	admins, moderators := h.voice.Roles()
	// Lists, never null in JSON.
	return roles{Admins: append([]uint32{}, admins...), Moderators: append([]uint32{}, moderators...), ChannelModerators: h.voice.ChannelModerators()}
}

func (r roles) forUser(h *Hub, id mumble.Identity) roles {
	r.ChannelModerators = slices.DeleteFunc(slices.Clone(r.ChannelModerators), func(m ChannelModerators) bool { return !h.voice.Sees(id.Session, m.Channel) })
	if r.ChannelModerators == nil {
		r.ChannelModerators = []ChannelModerators{}
	}
	return r
}

// RolesChanged sends everyone the new admins and moderators.
func (h *Hub) RolesChanged() {
	if h.voice == nil {
		return
	}
	all := h.roles()
	all.Type = "roles"
	h.eachReady(func(c *Conn, id mumble.Identity) { c.send(all.forUser(h, id)) })
	h.PrivateChanged() // staff see all channels
}

// listenOnly lists the listen-only voice channels (a list, never null in
// JSON; Voice gives speaker lists).
func (h *Hub) listenOnly() []ListenOnlyChannel {
	return append([]ListenOnlyChannel{}, h.voice.ListenOnly()...)
}

// listenOnlyFor: the listen-only channels the user sees.
func (h *Hub) listenOnlyFor(id mumble.Identity) []ListenOnlyChannel {
	return slices.DeleteFunc(h.listenOnly(), func(c ListenOnlyChannel) bool { return !h.voice.Sees(id.Session, c.Channel) })
}

// ListenOnlyChanged sends everyone the listen-only voice channels it sees.
func (h *Hub) ListenOnlyChanged() {
	if h.voice == nil {
		return
	}
	h.eachReady(func(c *Conn, id mumble.Identity) {
		c.send(map[string]any{"type": "listen-only", "channels": h.listenOnlyFor(id)})
	})
}

func (h *Hub) broadcast(v any) {
	for _, c := range h.all() {
		c.send(v)
	}
}

func (c *Conn) sendError(msg string) {
	c.send(map[string]any{"type": "error", "message": msg})
}

// manageError answers a management request (channels, roles): the client
// shows it apart from the chat.
func (c *Conn) manageError(msg string) {
	c.send(map[string]any{"type": "error", "message": msg, "about": "manage"})
}

var resync = []byte(`{"type":"resync"}`)

func (c *Conn) setAnswering(nonce string) {
	c.mu.Lock()
	c.answering = nonce
	c.mu.Unlock()
}

// send never blocks: a client that does not read its outbox loses messages
// instead of holding up everyone else. It gets a "resync" (reload what you
// show) as soon as there is room again.
func (c *Conn) send(v any) {
	if m, ok := v.(map[string]any); ok && m["type"] == "error" {
		c.mu.Lock()
		nonce := c.answering
		c.mu.Unlock()
		if nonce != "" {
			m = maps.Clone(m)
			m["nonce"] = nonce
			v = m
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("encode sidecar message", "err", err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if c.lagged {
		if len(c.out) > cap(c.out)-2 { // room for the resync and this message
			c.dropped++
			return
		}
		c.out <- resync
		c.lagged = false
	}
	select {
	case c.out <- b:
	default:
		c.lagged = true
		c.dropped++
		slog.Warn("sidecar outbox full, client will resync")
	}
}

// Overflows is the number of messages each connection lost to a full
// outbox, by voice server session (only those that lost some).
func (h *Hub) Overflows() map[uint32]uint64 {
	out := map[uint32]uint64{}
	for _, c := range h.all() {
		c.mu.Lock()
		if c.dropped > 0 && c.identity != nil {
			out[c.identity.Session] += c.dropped
		}
		c.mu.Unlock()
	}
	return out
}

// uploadTokens: one token, or `count` of them (at most store.MaxUploads)
// for the files of one message, in one request.
func (c *Conn) uploadTokens(id mumble.Identity, count int) {
	if count <= 1 {
		c.send(map[string]any{"type": "upload-token", "token": c.hub.issueUploadToken(id.Name, "")})
		return
	}
	tokens := make([]string, min(count, store.MaxUploads))
	for i := range tokens {
		tokens[i] = c.hub.issueUploadToken(id.Name, "")
	}
	c.send(map[string]any{"type": "upload-token", "token": tokens[0], "tokens": tokens})
}

func (h *Hub) issueUploadToken(author, emoji string) string {
	return h.issueToken(uploadToken{author: author, emoji: emoji})
}

func (h *Hub) issueToken(t uploadToken) string {
	b := make([]byte, 24)
	rand.Read(b)
	token := hex.EncodeToString(b)
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for t, v := range h.tokens { // drop expired tokens
		if now.After(v.expires) {
			delete(h.tokens, t)
		}
	}
	t.expires = now.Add(tokenLifetime)
	h.tokens[token] = t
	return token
}

// RedeemUploadToken returns the author a file upload token was issued to.
func (h *Hub) RedeemUploadToken(token string) (string, bool) {
	t, ok := h.redeem(token)
	if !ok || t.emoji != "" || t.personal || t.sticker != nil || t.logo || t.webhook != 0 {
		return "", false
	}
	return t.author, true
}

// redeem: a token works once and only before it expires.
func (h *Hub) redeem(token string) (uploadToken, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t, ok := h.tokens[token]
	delete(h.tokens, token)
	return t, ok && !h.now().After(t.expires)
}

// Text channel names as Discord has them: lower case, "-" for spaces.
var channelNameChars = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,32}$`)

func textChannelName(raw string) (string, bool) {
	name := strings.ToLower(strings.Join(strings.Fields(raw), "-"))
	return name, channelNameChars.MatchString(name)
}

const maxTopicLength = 200

// manage changes the text channels and categories (see mayManage) and
// sends everyone the new list.
func (c *Conn) manage(id mumble.Identity, req request) {
	h := c.hub
	// Authorized with the lock held: a channel moved meanwhile is checked
	// in its new category.
	h.manageMu.Lock()
	defer h.manageMu.Unlock()
	if !h.mayManage(id, req) {
		c.manageError("you may not manage channels")
		return
	}
	var err error
	taken := "a channel with this name exists"
	var private int64    // a new private text channel
	var privateCat int64 // a new private category
	// Private channels change too: a new one, or a private one deleted.
	access := false
	switch req.Type {
	case "create-channel", "rename-channel":
		name, ok := textChannelName(req.Name)
		if !ok {
			c.manageError("a channel name has 1 to 32 letters, digits, - or _")
			return
		}
		if req.Type == "create-channel" {
			var ch store.Channel
			// Only staff bring back a deleted channel's history (it may
			// have been private).
			if ch, err = h.store.CreateChannel(name, req.Category, h.voice.MayManage(id.Session)); err == nil && req.Private {
				private, access = ch.ID, true
			}
		} else {
			err = h.store.RenameChannel(req.Channel, name)
		}
	case "delete-channel":
		access = h.isPrivate(h.mirrorOf(req.Channel))
		err = h.store.HideChannel(req.Channel)
	case "set-topic":
		topic := strings.TrimSpace(req.Text)
		if utf8.RuneCountInString(topic) > maxTopicLength {
			c.manageError("a topic has at most 200 characters")
			return
		}
		err = h.store.SetTopic(req.Channel, topic)
	case "set-slow-mode":
		if !slices.Contains(slowModes, req.Seconds) {
			c.manageError("invalid slow mode")
			return
		}
		err = h.store.SetSlowMode(req.Channel, req.Seconds)
	case "order-channels":
		channels, _ := h.orderIDs()
		order := fullOrder(req.Channels, channels)
		if len(req.Channels) > 0 && h.categoryOf(req.Channels[0]) != 0 {
			order = inSlots(req.Channels, channels) // one category's: the others keep their places
		}
		err = h.store.SetPositions(order)
	case "create-category", "rename-category":
		name, ok := categoryName(req.Name)
		if !ok {
			c.manageError("a category name has 1 to 32 letters, digits, spaces or - _ = ( ) [ ] { } @ |")
			return
		}
		taken = "a category with this name exists"
		if req.Type == "create-category" {
			var cat store.Category
			if cat, err = h.store.CreateCategory(name); err == nil && req.Private {
				privateCat, access = cat.ID, true
			}
		} else {
			err = h.store.RenameCategory(req.Category, name)
		}
	case "delete-category": // its channels stay, without a category
		if h.exposes(req.Category, 0) {
			c.manageError("make the category public or move its channels first: they would become visible to everyone")
			return
		}
		access, err = true, h.store.DeleteCategory(req.Category)
	case "order-categories":
		_, cats := h.orderIDs()
		err = h.store.SetCategoryPositions(fullOrder(req.Cats, cats))
	case "set-category": // its moderators change
		if h.exposesChannel(req.Channel, req.Category) {
			c.manageError("this channel would become visible to everyone: make it private first")
			return
		}
		access, err = true, h.store.SetChannelCategory(req.Channel, req.Category)
	}
	switch {
	case errors.Is(err, store.ErrNameTaken):
		c.manageError(taken)
		return
	case err != nil:
		c.manageError("cannot change the channel")
		return
	}
	if err := h.syncMirrors(private); err != nil {
		slog.Error("text channel mirrors", "err", err)
	}
	if private != 0 && h.mirrorOf(private) == 0 {
		// Without its private mirror it would be public.
		if err := h.store.HideChannel(private); err != nil {
			slog.Error("hide a text channel", "err", err)
		}
		c.manageError("cannot make the private channel")
	}
	if err := h.reloadChannels(); err != nil {
		slog.Error("load text channels", "err", err)
		return
	}
	if privateCat != 0 {
		// Still empty: nothing is seen before it is private.
		h.mu.Lock()
		mumble := h.categoryMumble(privateCat)
		h.mu.Unlock()
		if mumble == 0 || h.voice.SetPrivate(id.Session, mumble, true, nil) != nil {
			c.manageError("the category was made, but not private: make it private in its settings")
		}
	}
	h.eachReady(func(c *Conn, id mumble.Identity) {
		if access {
			c.sendAccess(id)
		} else {
			c.send(h.channelsEvent(id))
		}
	})
}

// toWireChannels: mirrors only with the built-in server (the ids may be
// left from it when Murmur runs now).
func toWireChannels(channels []store.Channel, mirrors bool) []wireChannel {
	wire := make([]wireChannel, len(channels))
	for i, ch := range channels {
		wire[i] = wireChannel{ID: ch.ID, Name: ch.Name, Topic: ch.Topic, Category: ch.Category}
		if mirrors {
			wire[i].Mumble, wire[i].SlowMode = ch.Mumble, ch.SlowMode
		}
	}
	return wire
}

func (h *Hub) reloadChannels() error {
	channels, err := h.store.Channels()
	if err != nil {
		return err
	}
	cats, err := h.store.Categories()
	if err != nil {
		return err
	}
	wire := make([]wireCategory, len(cats))
	for i, c := range cats {
		wire[i] = wireCategory{ID: c.ID, Name: c.Name}
		if h.voice != nil {
			wire[i].Mumble = c.Mumble
		}
	}
	h.mu.Lock()
	h.channels, h.categories = toWireChannels(channels, h.voice != nil), wire
	h.mu.Unlock()
	return nil
}

// orderIDs: the text channels' and the categories' ids, in order.
func (h *Hub) orderIDs() (channels, categories []int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.channels {
		channels = append(channels, c.ID)
	}
	for _, c := range h.categories {
		categories = append(categories, c.ID)
	}
	return channels, categories
}

// known: the given ids that are in all, once each.
func known(given, all []int64) []int64 {
	var out []int64
	for _, id := range given {
		if slices.Contains(all, id) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// inSlots: all, with the given ids in the places where they stood, in
// the given order.
func inSlots(given, all []int64) []int64 {
	order := known(given, all)
	out, next := slices.Clone(all), order
	for i, id := range all {
		if slices.Contains(order, id) {
			out[i], next = next[0], next[1:]
		}
	}
	return out
}

// fullOrder: the given ids of all first, the others after them in their
// current order (a partial list must not leave two at one position).
func fullOrder(first, all []int64) []int64 {
	out := known(first, all)
	for _, id := range all {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
