package sidecar

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"

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

const agentPageSize = 100

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

func (h *Hub) ServeAgent(w http.ResponseWriter, r *http.Request) {
	var id mumble.Identity
	var write, ok bool
	if h.Agent != nil {
		id, write, ok = h.Agent(r)
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
			h.agentPost(w, r, id, ch, channel)
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
	if err != nil || limit < 1 || limit > agentPageSize {
		limit = pageSize
	}
	msgs, err := h.store.History(channel, before, limit+1)
	if err != nil {
		agentError(w, http.StatusInternalServerError, "cannot load history")
		return
	}
	more := len(msgs) > limit
	if more {
		msgs = msgs[1:]
	}
	wire := make([]wireMessage, len(msgs))
	for i, m := range msgs {
		wire[i] = h.toWireFor(m, viewerOf(id))
	}
	agentJSON(w, http.StatusOK, map[string]any{"messages": wire, "more": more})
}

// agentPost sends through Conn.post (rules, slow mode, time-outs, delivery)
// on a connection of its own that never gets ready: it receives only its
// own answers.
func (h *Hub) agentPost(w http.ResponseWriter, r *http.Request, id mumble.Identity, ch chat, channel int64) {
	var body struct {
		Text  string `json:"text"`
		Reply int64  `json:"reply"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body) != nil {
		agentError(w, http.StatusBadRequest, "send JSON: {\"text\": ..., \"reply\": message id}")
		return
	}
	var nonce [12]byte
	rand.Read(nonce[:])
	req := request{Type: "send", Channel: channel, Text: body.Text, Reply: body.Reply, Nonce: hex.EncodeToString(nonce[:])}
	c := h.Connect()
	defer c.Close()
	c.mu.Lock()
	c.identity = &id
	c.mu.Unlock()
	c.post(id, req, ch)
	m, err := h.store.MessageByNonce(store.NewMessage{ChannelID: channel, Author: id.Name, Verified: id.Registered, AuthorID: id.UserID, Nonce: req.Nonce, At: h.now()})
	if err == nil {
		agentJSON(w, http.StatusCreated, map[string]any{"message": h.toWireFor(m, viewerOf(id))})
		return
	}
	for {
		select {
		case raw := <-c.Outbox():
			var ev struct{ Type, Message string }
			if json.Unmarshal(raw, &ev) == nil && ev.Type == "error" {
				agentError(w, http.StatusBadRequest, ev.Message)
				return
			}
		default:
			agentError(w, http.StatusInternalServerError, "the message was not stored")
			return
		}
	}
}
