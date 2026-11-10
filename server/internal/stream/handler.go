package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/time/rate"

	"kallisti.chat/server/internal/config"
)

// Handler serves /api/stream: a WebSocket per voice session, separate from
// the voice connection so video never delays voice. The first message is
// {"type":"auth","token":...} with a token from the sidecar; then text
// commands (start, stop, watch, unwatch) and, from a streamer, binary
// frames (see Hub.Frame). Commands about the streamer's own stream say
// "camera": true for its camera; "session" names a watched stream by id.
type Handler struct {
	Hub            *Hub
	Redeem         func(token string) (session uint32, ok bool)
	AllowedOrigins []string
	// A streamer's upload (screen and camera together), at most (frames
	// above it are dropped).
	BytesPerSecond int
	// Dynamic stream limits (bytesPerSecond, maxSide, maxFps), if set.
	Limits func() (bytesPerSecond int, maxSide int, maxFps int)
}

const (
	authTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second // a peer that takes longer is gone
	outFrames    = 32               // frames queued per connection before it counts as full
	outTexts     = 32
	maxAuth      = 1024             // bytes, before the token is checked
	maxText      = maxPreview + 256 // a command (a preview is the largest)
	textsPerSec  = 10               // commands per second, steady
	soundsPerSec = 100              // sound frames per second (20 ms each: 50), steady
)

type command struct {
	Type    string `json:"type"`
	Token   string `json:"token"`
	Session uint32 `json:"session"` // watch, unwatch, keyframe: the stream's id
	Image   string `json:"image"`   // preview
	Config
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !config.OriginAllowed(h.AllowedOrigins, r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // the origin is checked above
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(maxAuth)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	authCtx, authCancel := context.WithTimeout(ctx, authTimeout)
	_, data, err := ws.Read(authCtx)
	authCancel()
	var auth command
	if err != nil || json.Unmarshal(data, &auth) != nil || auth.Type != "auth" {
		ws.Close(websocket.StatusPolicyViolation, "no authentication")
		return
	}
	session, ok := h.Redeem(auth.Token)
	if !ok {
		ws.Close(websocket.StatusPolicyViolation, "bad token")
		return
	}

	ws.SetReadLimit(maxFrame + headerSize)
	c := &conn{frames: make(chan []byte, outFrames), texts: make(chan []byte, outTexts), cancel: cancel}
	h.Hub.Connect(session, c)
	defer h.Hub.Disconnect(session, c)
	go c.write(ctx, ws)
	bps := h.BytesPerSecond
	side := maxSide
	fps := 60
	if h.Limits != nil {
		bps, side, fps = h.Limits()
	}
	// With the limits, for the streamer's encoders.
	c.SendText(map[string]any{"type": "ready", "limits": map[string]any{"bytesPerSecond": bps, "maxSide": side, "maxFps": fps}})
	bytes := rate.NewLimiter(rate.Limit(bps), max(256*1024, bps))
	texts := rate.NewLimiter(textsPerSec, 2*textsPerSec)
	sounds := rate.NewLimiter(soundsPerSec, soundsPerSec/2)
	fpsLimiter := rate.NewLimiter(rate.Limit(fps), max(10, 2*fps))
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			if h.Limits != nil {
				curBps, curSide, curFps := h.Limits()
				if curBps != bps || curSide != side || curFps != fps {
					bps, side, fps = curBps, curSide, curFps
					bytes.SetLimit(rate.Limit(bps))
					bytes.SetBurst(max(256*1024, bps))
					fpsLimiter.SetLimit(rate.Limit(fps))
					fpsLimiter.SetBurst(max(10, 2*fps))
					c.SendText(map[string]any{"type": "limits", "limits": map[string]any{"bytesPerSecond": bps, "maxSide": side, "maxFps": fps}})
				}
			}
			if len(data) > 0 && data[0]&audioFlag != 0 && !sounds.Allow() {
				continue // many tiny sound frames would multiply at every listener
			}
			isAudio := len(data) > 0 && data[0]&audioFlag != 0
			isKey := len(data) > 0 && data[0]&keyFlag != 0
			if !isAudio && !isKey && !fpsLimiter.Allow() {
				h.Hub.Dropped(FrameID(session, data))
				continue
			}
			if bytes.AllowN(time.Now(), len(data)) {
				h.Hub.Frame(session, data)
			} else {
				h.Hub.Dropped(FrameID(session, data))
			}
			continue
		}
		var cmd command
		if len(data) > maxText || !texts.Allow() || json.Unmarshal(data, &cmd) != nil {
			continue
		}
		switch cmd.Type {
		case "start":
			if err := h.Hub.Start(session, cmd.Config); err != nil {
				c.SendText(map[string]any{"type": "error", "message": err.Error(), "camera": cmd.Camera})
			} else {
				c.SendText(map[string]any{"type": "started", "camera": cmd.Camera})
			}
		case "stop":
			h.Hub.Stop(ID(session, cmd.Camera))
		case "watch":
			if err := h.Hub.Watch(session, cmd.Session); err != nil {
				c.SendText(map[string]any{"type": "error", "message": err.Error(), "session": cmd.Session})
			}
		case "unwatch":
			h.Hub.Unwatch(session, cmd.Session)
		case "preview":
			h.Hub.SetPreview(ID(session, cmd.Camera), cmd.Image) // a bad one is dropped
		case "keyframe": // a viewer's decoder needs one
			h.Hub.RequestKey(session, cmd.Session)
		}
	}
}

// conn queues what goes out; it never blocks the hub.
type conn struct {
	frames chan []byte
	texts  chan []byte
	cancel func() // ends the connection
}

func (c *conn) Close() { c.cancel() }

func (c *conn) SendFrame(b []byte) bool {
	select {
	case c.frames <- b:
		return true
	default:
		return false
	}
}

func (c *conn) Room() bool { return len(c.frames) <= outFrames/4 }

func (c *conn) SendText(v any) {
	b, _ := json.Marshal(v) // our own messages always marshal
	select {
	case c.texts <- b:
	default:
		slog.Warn("stream connection full, a message was dropped")
	}
}

func (c *conn) write(ctx context.Context, ws *websocket.Conn) {
	defer c.cancel()
	for {
		var typ websocket.MessageType
		var b []byte
		select { // control messages first
		case b = <-c.texts:
			typ = websocket.MessageText
		default:
			select {
			case b = <-c.texts:
				typ = websocket.MessageText
			case b = <-c.frames:
				typ = websocket.MessageBinary
			case <-ctx.Done():
				return
			}
		}
		wctx, done := context.WithTimeout(ctx, writeTimeout)
		err := ws.Write(wctx, typ, b)
		done()
		if err != nil {
			return
		}
	}
}

// Tokens are single-use, short-lived stream tokens for voice sessions,
// issued over the authenticated voice connection (the sidecar).
type Tokens struct {
	mu     sync.Mutex
	tokens map[string]token
	now    func() time.Time
}

type token struct {
	session uint32
	expires time.Time
}

const tokenLifetime = time.Minute

func NewTokens() *Tokens { return &Tokens{tokens: map[string]token{}, now: time.Now} }

func (t *Tokens) Issue(session uint32) string {
	b := make([]byte, 24)
	rand.Read(b)
	s := hex.EncodeToString(b)
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for k, v := range t.tokens {
		if now.After(v.expires) {
			delete(t.tokens, k)
		}
	}
	t.tokens[s] = token{session: session, expires: now.Add(tokenLifetime)}
	return s
}

func (t *Tokens) Redeem(s string) (uint32, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.tokens[s]
	delete(t.tokens, s)
	if !ok || t.now().After(v.expires) {
		return 0, false
	}
	return v.session, true
}
