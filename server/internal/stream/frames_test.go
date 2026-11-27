package stream

import (
	"bytes"
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

// testFrames: keyframes of many sizes, with sound frames between them, each
// with its own bytes; a later, smaller one is read over an earlier one.
func testFrames() [][]byte {
	var out [][]byte
	for i, size := range []int{200 << 10, 30 << 10, 1 << 10, 100 << 10, 17 << 10, 300 << 10, 64, 50 << 10} {
		for j, f := range [][]byte{make([]byte, headerSize+size), make([]byte, headerSize+100+i*40)} {
			f[0] = keyFlag
			if j == 1 {
				f[0] = audioFlag
			}
			f[8] = byte(i)
			for k := headerSize; k < len(f); k++ {
				f[k] = byte(i*31 + j*7 + k)
			}
			out = append(out, f)
		}
	}
	return out
}

// A frame is read into a buffer that the next one overwrites: viewers that
// read slowly (their frames wait in the queue) still get every frame whole.
func TestSlowViewersGetEveryFrameWhole(t *testing.T) {
	hub := New(&fakeVoice{channels: map[uint32]uint32{1: 5, 2: 5, 3: 5}})
	tokens := NewTokens()
	srv := httptest.NewServer(&Handler{Hub: hub, Redeem: tokens.Redeem, AllowedOrigins: []string{"*"}, BytesPerSecond: 1 << 26})
	defer srv.Close()
	ctx := context.Background()
	streamer := dialStream(t, srv, tokens.Issue(1))
	streamer.Write(ctx, websocket.MessageText, []byte(`{"type":"start","codec":"vp8","width":640,"height":360,"audio":"opus"}`))
	readText(t, streamer, "started")
	var viewers []*websocket.Conn
	for _, s := range []uint32{2, 3} {
		v := dialStream(t, srv, tokens.Issue(s))
		v.SetReadLimit(-1)
		v.Write(ctx, websocket.MessageText, []byte(`{"type":"watch","session":1}`))
		readText(t, v, "stream")
		viewers = append(viewers, v)
	}
	frames := testFrames()
	if len(frames) > outFrames {
		t.Fatalf("%d frames would not all wait in a queue", len(frames))
	}
	for _, f := range frames {
		if err := streamer.Write(ctx, websocket.MessageBinary, f); err != nil {
			t.Fatal(err)
		}
	}
	streamer.Write(ctx, websocket.MessageText, []byte(`{"type":"watch","session":7}`))
	readText(t, streamer, "error") // every frame was read
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for n, v := range viewers {
		for i := 0; i < len(frames); {
			typ, data, err := v.Read(rctx)
			if err != nil {
				t.Fatalf("viewer %d, frame %d: %v", n, i, err)
			}
			if typ != websocket.MessageBinary {
				continue
			}
			if want := append([]byte{0, 0, 0, 1}, frames[i]...); !bytes.Equal(data, want) {
				t.Fatalf("viewer %d, frame %d: got %d bytes, want %d (or other bytes)", n, i, len(data), len(want))
			}
			i++
		}
	}
}

// OnSound gets each sound frame's bytes, though the buffer is the next
// frame's once it returns.
func TestSoundReachesOnSoundWhole(t *testing.T) {
	hub := New(&fakeVoice{channels: map[uint32]uint32{1: 5}})
	got := make(chan []byte, 64)
	hub.OnSound = func(_ uint32, opus []byte, _ uint64) { got <- bytes.Clone(opus) }
	tokens := NewTokens()
	srv := httptest.NewServer(&Handler{Hub: hub, Redeem: tokens.Redeem, AllowedOrigins: []string{"*"}, BytesPerSecond: 1 << 26})
	defer srv.Close()
	ctx := context.Background()
	streamer := dialStream(t, srv, tokens.Issue(1))
	streamer.Write(ctx, websocket.MessageText, []byte(`{"type":"start","codec":"vp8","width":640,"height":360,"audio":"opus"}`))
	readText(t, streamer, "started")
	var want [][]byte
	for _, f := range testFrames() {
		if f[0]&audioFlag != 0 {
			want = append(want, f[headerSize:])
		}
		streamer.Write(ctx, websocket.MessageBinary, f)
	}
	streamer.Write(ctx, websocket.MessageText, []byte(`{"type":"watch","session":7}`))
	readText(t, streamer, "error")
	for i, w := range want {
		select {
		case g := <-got:
			if !bytes.Equal(g, w) {
				t.Fatalf("sound frame %d: got %d bytes, want %d (or other bytes)", i, len(g), len(w))
			}
		default:
			t.Fatalf("sound frame %d is missing", i)
		}
	}
}

// A frame of the largest size is read; one byte more ends the connection.
func TestAFrameAboveTheReadLimitEndsTheConnection(t *testing.T) {
	hub := New(&fakeVoice{channels: map[uint32]uint32{1: 5}})
	tokens := NewTokens()
	srv := newTestServer(t, hub, tokens)
	ctx := context.Background()
	streamer := dialStream(t, srv, tokens.Issue(1))
	streamer.Write(ctx, websocket.MessageBinary, make([]byte, maxFrame+headerSize))
	streamer.Write(ctx, websocket.MessageText, []byte(`{"type":"watch","session":7}`))
	readText(t, streamer, "error")
	streamer.Write(ctx, websocket.MessageBinary, make([]byte, maxFrame+headerSize+1))
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		_, _, err := streamer.Read(rctx)
		if err != nil {
			if s := websocket.CloseStatus(err); s != websocket.StatusMessageTooBig {
				t.Fatalf("closed with %v: %v", s, err)
			}
			return
		}
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
