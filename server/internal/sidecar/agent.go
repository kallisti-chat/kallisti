package sidecar

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// The agent API (docs/agent-api.md): programs read and post as an account
// with a personal access token, through the same checks as the client
// (Hub.may, Conn.post), without a Mumble connection.
//
//	GET  /api/agent/channels
//	GET  /api/agent/channels/{id}/messages?before=&limit=
//	POST /api/agent/channels/{id}/messages {"text", "reply"}
//	POST /api/agent/channels/{id}/messages/{message}/reactions {"emoji", "on"}
//	GET  /api/agent/events (server-sent events)

const agentPageSize = 100

// Event streams: at most agentStreams per account; a comment line every
// agentKeepAlive keeps proxies from closing them, and the token is
// checked again then.
const (
	agentStreams      = 4
	agentWriteTimeout = 10 * time.Second
)

var agentKeepAlive = 25 * time.Second

// agentEvents are the events of a chat's readers that a stream passes on.
var agentEvents = map[string]bool{"message": true, "message-edited": true, "message-deleted": true, "messages-deleted": true, "reactions": true}

// agentLimits: one rate limit per account, as a client connection has.
type agentLimits struct {
	mu sync.Mutex
	m  map[uint32]*rate.Limiter
}

func (l *agentLimits) allow(user uint32) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[uint32]*rate.Limiter{}
	}
	if l.m[user] == nil {
		l.m[user] = rate.NewLimiter(perSecond, burst)
	}
	return l.m[user].Allow()
}

func agentJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func agentError(w http.ResponseWriter, code int, message string) {
	agentJSON(w, code, map[string]string{"error": message})
}

// agentOf is a request's token as an identity (see Hub.Agent). banned:
// the account's user is banned; a token does not get around that.
func (h *Hub) agentOf(r *http.Request) (id mumble.Identity, write, banned, ok bool) {
	if h.Agent == nil {
		return
	}
	if id, write, ok = h.Agent(r); ok && h.voice != nil && h.voice.UserBanned(id.UserID) {
		return id, false, true, false
	}
	return
}

func (h *Hub) ServeAgent(w http.ResponseWriter, r *http.Request) {
	id, write, banned, ok := h.agentOf(r)
	if banned {
		agentError(w, http.StatusForbidden, "this account is banned")
		return
	}
	if !ok {
		agentError(w, http.StatusUnauthorized, "a valid access token is needed (Authorization: Bearer)")
		return
	}
	if !h.agentLimits.allow(id.UserID) {
		agentError(w, http.StatusTooManyRequests, "too many requests, slow down")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/agent/"), "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "channels" && r.Method == http.MethodGet:
		agentJSON(w, http.StatusOK, map[string]any{"channels": h.channelsFor(id)})
	case len(parts) == 1 && parts[0] == "events" && r.Method == http.MethodGet:
		h.agentStream(w, r, id)
	case len(parts) == 5 && parts[0] == "channels" && parts[2] == "messages" && parts[4] == "reactions" && r.Method == http.MethodPost:
		channel, _ := strconv.ParseInt(parts[1], 10, 64)
		message, _ := strconv.ParseInt(parts[3], 10, 64)
		m, err := h.store.MessageByID(message)
		ch, found := h.chatOf(channel)
		switch {
		case err != nil || !found || m.ChannelID != channel || !h.may(id, ch, false):
			agentError(w, http.StatusNotFound, "no such message")
		case !write:
			agentError(w, http.StatusForbidden, "this token may only read")
		default:
			h.agentReact(w, r, id, message)
		}
	case len(parts) == 3 && parts[0] == "channels" && parts[2] == "messages":
		channel, err := strconv.ParseInt(parts[1], 10, 64)
		ch, found := h.chatOf(channel)
		if err != nil || !found || !h.may(id, ch, false) {
			agentError(w, http.StatusNotFound, "no such channel")
			return
		}
		switch r.Method {
		case http.MethodGet:
			h.agentHistory(w, r, id, channel)
		case http.MethodPost:
			if !write {
				agentError(w, http.StatusForbidden, "this token may only read")
				return
			}
			h.agentPost(w, r, id, channel)
		default:
			agentError(w, http.StatusMethodNotAllowed, "GET or POST")
		}
	default:
		agentError(w, http.StatusNotFound, "no such endpoint")
	}
}

func (h *Hub) agentHistory(w http.ResponseWriter, r *http.Request, id mumble.Identity, channel int64) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 {
		limit = pageSize
	}
	wire, more, err := h.historyPage(channel, before, min(limit, agentPageSize), viewerOf(id))
	if err != nil {
		agentError(w, http.StatusInternalServerError, "cannot load history")
		return
	}
	agentJSON(w, http.StatusOK, map[string]any{"messages": wire, "more": more})
}

// agentPost sends through Conn.dispatch (the client's gates and delivery)
// on a connection of its own that never gets ready: it receives only its
// own answers.
func (h *Hub) agentPost(w http.ResponseWriter, r *http.Request, id mumble.Identity, channel int64) {
	var body struct {
		Text  string `json:"text"`
		Reply int64  `json:"reply"`
		// The same on a retry: the post is stored once (see store.MessageByNonce).
		Nonce string `json:"nonce"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body) != nil {
		agentError(w, http.StatusBadRequest, "send JSON: {\"text\": ..., \"reply\": message id}")
		return
	}
	if body.Nonce == "" {
		var nonce [12]byte
		rand.Read(nonce[:])
		body.Nonce = hex.EncodeToString(nonce[:])
	}
	req := request{Type: "send", Channel: channel, Text: body.Text, Reply: body.Reply, Nonce: body.Nonce, bot: true}
	refused := h.agentDo(w, id, req)
	m, err := h.store.MessageByNonce(store.NewMessage{ChannelID: channel, Author: id.Name, Verified: id.Registered, AuthorID: id.UserID, Nonce: req.Nonce, At: h.now()})
	switch {
	case err == nil:
		agentJSON(w, http.StatusCreated, map[string]any{"message": h.toWireFor(m, viewerOf(id))})
	case !refused():
		agentError(w, http.StatusInternalServerError, "the message was not stored")
	}
}

// agentReact adds a reaction to a message, or removes it ("on": false).
func (h *Hub) agentReact(w http.ResponseWriter, r *http.Request, id mumble.Identity, message int64) {
	var body struct {
		Emoji string `json:"emoji"`
		On    *bool  `json:"on"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
		agentError(w, http.StatusBadRequest, "send JSON: {\"emoji\": ..., \"on\": true or false}")
		return
	}
	if !h.agentDo(w, id, request{Type: "react", Message: message, Emoji: body.Emoji, On: body.On == nil || *body.On})() {
		w.WriteHeader(http.StatusNoContent)
	}
}

// agentDo runs a request through Conn.dispatch (the client's gates and
// delivery) on a connection of its own that never gets ready: it receives
// only its own answers. refused answers with the error the request got,
// if it got one.
func (h *Hub) agentDo(w http.ResponseWriter, id mumble.Identity, req request) (refused func() bool) {
	c := h.Connect()
	defer c.Close()
	c.mu.Lock()
	c.identity = &id
	c.mu.Unlock()
	c.dispatch(id, req)
	var answers [][]byte
	for {
		select {
		case raw := <-c.Outbox():
			answers = append(answers, raw)
			continue
		default:
		}
		break
	}
	return func() bool {
		for _, raw := range answers {
			var ev struct{ Type, Message, About string }
			if json.Unmarshal(raw, &ev) == nil && ev.Type == "error" {
				code := http.StatusBadRequest
				if ev.About == "slow-mode" || ev.About == "rate" {
					code = http.StatusTooManyRequests // back off and try again
				}
				agentError(w, code, ev.Message)
				return true
			}
		}
		return false
	}
}

// CloseStreams ends the event streams, at shutdown: the HTTP server waits
// for open requests (not for WebSockets).
func (h *Hub) CloseStreams() {
	h.mu.Lock()
	streams := slices.Collect(maps.Keys(h.streams))
	h.mu.Unlock()
	for _, c := range streams {
		c.Close()
	}
}

// agentStream sends what the readers of the account's chats get, as
// server-sent events, until the client leaves, the token ends, or the
// client reads too slowly (it then connects again and pages the history).
func (h *Hub) agentStream(w http.ResponseWriter, r *http.Request, id mumble.Identity) {
	c := h.Connect()
	defer c.Close()
	c.mu.Lock()
	c.identity = &id
	c.mu.Unlock()
	h.mu.Lock()
	open := 0
	for _, user := range h.streams {
		if user == id.UserID {
			open++
		}
	}
	if open < agentStreams {
		h.streams[c] = id.UserID
	}
	h.mu.Unlock()
	if open >= agentStreams {
		agentError(w, http.StatusTooManyRequests, "too many event streams for this account")
		return
	}
	out := http.NewResponseController(w)
	write := func(text string) bool {
		out.SetWriteDeadline(time.Now().Add(agentWriteTimeout))
		_, err := io.WriteString(w, text)
		return err == nil && out.Flush() == nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: do not hold events back
	if !write("event: ready\ndata: {}\n\n") {
		return
	}
	tick := time.NewTicker(agentKeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			now, _, _, ok := h.agentOf(r)
			if !ok || now.UserID != id.UserID || !write(": ping\n\n") {
				return
			}
			c.mu.Lock()
			c.identity = &now
			c.mu.Unlock()
		case raw, ok := <-c.Outbox():
			var ev struct{ Type string }
			if !ok || json.Unmarshal(raw, &ev) != nil || ev.Type == "resync" {
				return
			}
			if agentEvents[ev.Type] && !write("event: "+ev.Type+"\ndata: "+string(raw)+"\n\n") {
				return
			}
		}
	}
}
