package sidecar

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/mumble"
)

// agentServer: "Bearer w" writes and "Bearer r" reads, as alice (session 5).
func agentServer(t *testing.T, h *Hub) *httptest.Server {
	h.Agent = func(r *http.Request) (mumble.Identity, bool, bool) {
		id := mumble.Identity{Session: 5, Name: "alice", Registered: true, UserID: 7}
		switch r.Header.Get("Authorization") {
		case "Bearer w":
			return id, true, true
		case "Bearer r":
			return id, false, true
		}
		return mumble.Identity{}, false, false
	}
	srv := httptest.NewServer(http.HandlerFunc(h.ServeAgent))
	t.Cleanup(srv.Close)
	return srv
}

func agentCall(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

func TestAgentAPI(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	srv := agentServer(t, h)
	bob := readyAs(t, h, 2, "bob")
	general := h.channels[0].ID

	if code, _ := agentCall(t, srv, "GET", "/api/agent/channels", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	code, v := agentCall(t, srv, "GET", "/api/agent/channels", "r", nil)
	if list, _ := v["channels"].([]any); code != 200 || len(list) != 1 {
		t.Fatalf("channels: %d %v", code, v)
	}
	path := fmt.Sprintf("/api/agent/channels/%d/messages", general)

	// Posting needs a write token.
	if code, _ := agentCall(t, srv, "POST", path, "r", map[string]any{"text": "hi"}); code != http.StatusForbidden {
		t.Fatalf("read token posted: %d", code)
	}
	code, v = agentCall(t, srv, "POST", path, "w", map[string]any{"text": "hello from an agent"})
	msg, _ := v["message"].(map[string]any)
	if code != http.StatusCreated || msg["text"] != "hello from an agent" || msg["author"] != "alice" || msg["bot"] != true {
		t.Fatalf("post: %d %v", code, v)
	}
	// Others see it live, as any message.
	if live := nextOf(t, bob, "message")["message"].(map[string]any); live["id"] != msg["id"] {
		t.Fatalf("bob got %v", live)
	}
	// A reply, and the history has both.
	code, v = agentCall(t, srv, "POST", path, "w", map[string]any{"text": "a reply", "reply": msg["id"]})
	if code != http.StatusCreated {
		t.Fatalf("reply: %d %v", code, v)
	}
	code, v = agentCall(t, srv, "GET", path, "r", nil)
	if list, _ := v["messages"].([]any); code != 200 || len(list) != 2 {
		t.Fatalf("history: %d %v", code, v)
	}
	// Refusals come back with the reason.
	if code, v := agentCall(t, srv, "POST", path, "w", map[string]any{"text": ""}); code != http.StatusBadRequest || v["error"] == "" {
		t.Fatalf("empty text: %d %v", code, v)
	}
	if code, _ := agentCall(t, srv, "GET", "/api/agent/channels/999/messages", "r", nil); code != http.StatusNotFound {
		t.Fatalf("unknown channel: %d", code)
	}
}

// A token posts through the client's gates: here read-only guests.
func TestAgentPostsThroughTheGates(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{guests: map[uint32]bool{5: true}, guestsReadOnly: true}, "")
	srv := agentServer(t, h)
	path := fmt.Sprintf("/api/agent/channels/%d/messages", h.channels[0].ID)
	if code, v := agentCall(t, srv, "POST", path, "w", map[string]any{"text": "hi"}); code != http.StatusBadRequest {
		t.Fatalf("read-only guest posted: %d %v", code, v)
	}
}

// openAgentEvents opens the event stream; next gives the next event.
func openAgentEvents(t *testing.T, srv *httptest.Server, token string) (next func() (string, map[string]any), code int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/agent/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	lines := bufio.NewScanner(resp.Body)
	return func() (string, map[string]any) {
		t.Helper()
		var event string
		for lines.Scan() {
			line := lines.Text()
			if name, ok := strings.CutPrefix(line, "event: "); ok {
				event = name
			} else if data, ok := strings.CutPrefix(line, "data: "); ok {
				var v map[string]any
				json.Unmarshal([]byte(data), &v)
				return event, v
			}
		}
		return "", nil // the stream ended
	}, resp.StatusCode
}

// The event stream: new messages and reactions of the chats the account
// may read, as the app gets them.
func TestAgentEvents(t *testing.T) {
	h, _, _ := privateHub(t) // #secret is hidden from alice (user 7)
	srv := agentServer(t, h)
	bob := joinAs(t, h, mumble.Identity{Session: 1, Name: "bob", Registered: true, UserID: 20}) // session 1 sees it
	general, secret := h.channels[0].ID, h.channels[1].ID
	next, code := openAgentEvents(t, srv, "r")
	if code != http.StatusOK {
		t.Fatalf("stream: %d", code)
	}
	if event, v := next(); event != "ready" || v == nil {
		t.Fatalf("first: %s %v", event, v)
	}
	postTo(t, h, bob, secret, "not for alice")
	id := postTo(t, h, bob, general, "hello")
	event, v := next()
	if msg, _ := v["message"].(map[string]any); event != "message" || msg["text"] != "hello" {
		t.Fatalf("got %s %v", event, v)
	}
	// A reaction, through the API (it needs a token that may post).
	path := fmt.Sprintf("/api/agent/channels/%d/messages/%d/reactions", general, id)
	if code, _ := agentCall(t, srv, "POST", path, "r", map[string]any{"emoji": "👍"}); code != http.StatusForbidden {
		t.Fatalf("read token reacted: %d", code)
	}
	if code, v := agentCall(t, srv, "POST", path, "w", map[string]any{"emoji": "👍"}); code != http.StatusNoContent {
		t.Fatalf("react: %d %v", code, v)
	}
	event, v = next()
	if list, _ := v["reactions"].([]any); event != "reactions" || len(list) != 1 {
		t.Fatalf("got %s %v", event, v)
	}
	if code, _ := agentCall(t, srv, "POST", path, "w", map[string]any{"emoji": "👍", "on": false}); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if event, v = next(); event != "reactions" || v["reactions"] != nil && len(v["reactions"].([]any)) != 0 {
		t.Fatalf("got %s %v", event, v)
	}
	// Refusals: not an emoji, a message of another channel.
	if code, v := agentCall(t, srv, "POST", path, "w", map[string]any{"emoji": "nope"}); code != http.StatusBadRequest || v["error"] == "" {
		t.Fatalf("bad emoji: %d %v", code, v)
	}
	other := fmt.Sprintf("/api/agent/channels/%d/messages/%d/reactions", general, id+999)
	if code, _ := agentCall(t, srv, "POST", other, "w", map[string]any{"emoji": "👍"}); code != http.StatusNotFound {
		t.Fatalf("unknown message: %d", code)
	}
}

// At most agentStreams streams per account.
func TestAgentEventStreamsAreLimited(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	srv := agentServer(t, h)
	for range agentStreams {
		if _, code := openAgentEvents(t, srv, "r"); code != http.StatusOK {
			t.Fatalf("stream: %d", code)
		}
	}
	if _, code := openAgentEvents(t, srv, "r"); code != http.StatusTooManyRequests {
		t.Fatalf("one too many: %d", code)
	}
}

// A banned account's token does nothing, and its open stream ends.
func TestAgentAPIRefusesBannedAccounts(t *testing.T) {
	h := newHub(t, "general")
	v := &fakeVoice{}
	h.SetVoice(v, "")
	srv := agentServer(t, h)
	old := agentKeepAlive
	agentKeepAlive = 20 * time.Millisecond
	t.Cleanup(func() { agentKeepAlive = old })
	next, _ := openAgentEvents(t, srv, "r")
	next() // ready
	v.mu.Lock()
	v.banned = map[uint32]bool{7: true}
	v.mu.Unlock()
	if code, _ := agentCall(t, srv, "GET", "/api/agent/channels", "r", nil); code != http.StatusForbidden {
		t.Fatalf("banned: %d", code)
	}
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for event, _ := next(); event != ""; event, _ = next() {
		}
	}()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream stays open")
	}
}

// At shutdown the streams end: the HTTP server waits for open requests.
func TestCloseStreamsEndsTheEventStreams(t *testing.T) {
	h := newHub(t, "general")
	h.SetVoice(&fakeVoice{}, "")
	next, _ := openAgentEvents(t, agentServer(t, h), "r")
	next() // ready
	h.CloseStreams()
	if event, _ := next(); event != "" {
		t.Fatalf("got %q after the close", event)
	}
}
