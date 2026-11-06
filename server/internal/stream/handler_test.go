package stream

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func dialStream(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	ws.Write(ctx, websocket.MessageText, []byte(`{"type":"auth","token":"`+token+`"}`))
	return ws
}

func readText(t *testing.T, ws *websocket.Conn, want string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var m map[string]any
		json.Unmarshal(data, &m)
		if m["type"] == want {
			return m
		}
	}
}

func TestHandlerForwardsFrames(t *testing.T) {
	hub := New(&fakeVoice{channels: map[uint32]uint32{1: 5, 2: 5}})
	tokens := NewTokens()
	srv := httptest.NewServer(&Handler{Hub: hub, Redeem: tokens.Redeem, AllowedOrigins: []string{"*"}, BytesPerSecond: 1 << 20})
	defer srv.Close()
	streamer := dialStream(t, srv, tokens.Issue(1))
	viewer := dialStream(t, srv, tokens.Issue(2))
	ctx := context.Background()
	streamer.Write(ctx, websocket.MessageText, []byte(`{"type":"start","codec":"vp8","width":640,"height":360}`))
	readText(t, streamer, "started")
	viewer.Write(ctx, websocket.MessageText, []byte(`{"type":"watch","session":1}`))
	readText(t, viewer, "stream")
	readText(t, streamer, "keyframe")
	streamer.Write(ctx, websocket.MessageBinary, frame(true, 42))
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		typ, data, err := viewer.Read(rctx)
		if err != nil {
			t.Fatal(err)
		}
		if typ == websocket.MessageBinary {
			if data[3] != 1 || data[len(data)-1] != 42 {
				t.Fatalf("frame %v", data)
			}
			break
		}
	}
	// A token works once.
	again := dialStream(t, srv, "nope")
	if _, _, err := again.Read(rctx); err == nil {
		t.Fatal("a bad token was let in")
	}
}

func TestHandlerDynamicLimits(t *testing.T) {
	hub := New(&fakeVoice{channels: map[uint32]uint32{1: 5}})
	tokens := NewTokens()
	srv := httptest.NewServer(&Handler{
		Hub:            hub,
		Redeem:         tokens.Redeem,
		AllowedOrigins: []string{"*"},
		BytesPerSecond: 1 << 20,
		Limits: func() (int, int, int) {
			return 2_000_000, 1080, 30
		},
	})
	defer srv.Close()
	ws := dialStream(t, srv, tokens.Issue(1))
	ready := readText(t, ws, "ready")
	limits, ok := ready["limits"].(map[string]any)
	if !ok || limits["bytesPerSecond"] != float64(2_000_000) || limits["maxSide"] != float64(1080) || limits["maxFps"] != float64(30) {
		t.Fatalf("expected dynamic limits, got %v", ready)
	}
}
