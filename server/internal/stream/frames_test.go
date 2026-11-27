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

// BenchmarkStreamerFrames measures reading a streamer's frames: no stream
// is started, so the hub drops each one at once and the read is what shows.
// Sizes: an Opus frame, a delta frame and a keyframe at the default budget.
func BenchmarkStreamerFrames(b *testing.B) {
	for _, c := range []struct {
		name  string
		flags byte
		size  int
	}{
		{"sound160", audioFlag, 160},
		{"delta30K", 0, 30 << 10},
		{"key200K", keyFlag, 200 << 10},
	} {
		b.Run(c.name, func(b *testing.B) {
			hub := New(&fakeVoice{channels: map[uint32]uint32{1: 5}})
			tokens := NewTokens()
			srv := httptest.NewServer(&Handler{Hub: hub, Redeem: tokens.Redeem, AllowedOrigins: []string{"*"}, BytesPerSecond: 1 << 30})
			defer srv.Close()
			ctx := context.Background()
			ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				b.Fatal(err)
			}
			defer ws.CloseNow()
			ws.Write(ctx, websocket.MessageText, []byte(`{"type":"auth","token":"`+tokens.Issue(1)+`"}`))
			waitText(b, ws, "ready")
			f := make([]byte, headerSize+c.size)
			f[0] = c.flags
			b.SetBytes(int64(len(f)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := ws.Write(ctx, websocket.MessageBinary, f); err != nil {
					b.Fatal(err)
				}
			}
			// The reply comes after every frame was read.
			ws.Write(ctx, websocket.MessageText, []byte(`{"type":"watch","session":7}`))
			waitText(b, ws, "error")
			b.StopTimer()
		})
	}
}

func waitText(tb testing.TB, ws *websocket.Conn, want string) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			tb.Fatalf("waiting for %q: %v", want, err)
		}
		var m map[string]any
		if typ == websocket.MessageText && json.Unmarshal(data, &m) == nil && m["type"] == want {
			return
		}
	}
}
