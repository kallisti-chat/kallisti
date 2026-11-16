package sidecar

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
