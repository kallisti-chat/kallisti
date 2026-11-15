// Package whip takes streams from OBS (WHIP, RFC 9725): an encoder posts
// an SDP offer with its owner's stream key and sends H.264 and Opus over
// WebRTC. The stream shows as a screen share of the owner's voice session
// (see stream.Ingest): pictures and sound are forwarded, never decoded.
package whip

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"

	"kallisti.chat/server/internal/stream"
)

const Path = "/api/whip"

// Handler serves Path: POST with the stream key as a Bearer token and an
// SDP offer gives the answer and the session's URL (Location); DELETE on
// that URL ends the stream.
type Handler struct {
	Settings webrtc.SettingEngine // see Settings
	Hub      *stream.Hub
	// Account: a stream key's account name. Session: the account's voice
	// session (registered and connected).
	Account func(key string) (name string, ok bool)
	Session func(name string) (uint32, bool)
	// A stream's pictures, at most (above it they are dropped), and the
	// sessions at the same time.
	BytesPerSecond int
	Limits         func() (bytesPerSec, maxSide, maxFps int)
	MaxSessions    int
	// For the offer's body, until the peer connects, then until its first
	// keyframe, between pictures, and for the owner's voice session after
	// the stream ended (0: the defaults).
	OfferTimeout, ConnectTimeout, KeyTimeout, MediaTimeout, RejoinTimeout time.Duration
	Closer                                                                io.Closer // the UDP port (see Listen), closed by Close; nil: none
	// Preview makes a JPEG of a keyframe for the stream's preview (nil:
	// none).
	Preview func(ctx context.Context, keyframe []byte) ([]byte, error)
	// Warn tells the owner's voice session about a problem of its stream,
	// such as B-frames (nil: nobody).
	Warn func(session uint32, text string)

	mu       sync.Mutex
	sessions map[string]*session
	// Offers still coming (at most MaxSessions), by account: a new key
	// stops reading them.
	pending map[*http.ResponseController]string
	closed  bool
}

const (
	maxOffer       = 64 << 10
	offerTimeout   = 10 * time.Second
	connectTimeout = 15 * time.Second
	keyTimeout     = 15 * time.Second
	mediaTimeout   = 10 * time.Second
	rejoinTimeout  = 20 * time.Second // a browser refresh, a reconnect
	gatherTimeout  = 5 * time.Second
)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch id, sub := strings.CutPrefix(r.URL.Path, Path+"/"); {
	case r.URL.Path == Path && r.Method == http.MethodPost:
		h.start(w, r)
	case r.URL.Path == Path && r.Method == http.MethodOptions:
		w.Header().Set("Accept-Post", "application/sdp")
		w.WriteHeader(http.StatusNoContent)
	case sub && r.Method == http.MethodDelete:
		if s := h.take(id); s != nil {
			s.close()
			w.WriteHeader(http.StatusOK)
		} else {
			http.Error(w, "no such stream", http.StatusNotFound)
		}
	case sub || r.URL.Path == Path: // PATCH (trickle ICE) too: not supported
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	// The offer's deadline (see readOffer), also for an early refusal: else
	// net/http waits for the unread rest of the body.
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(cmp.Or(h.OfferTimeout, offerTimeout)))
	key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	key = strings.TrimSpace(key)
	name, ok := h.Account(key)
	if !ok {
		unauthorized(w)
		return
	}
	if ctype, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ctype != "application/sdp" {
		http.Error(w, "send an SDP offer (application/sdp)", http.StatusUnsupportedMediaType)
		return
	}
	body, status, err := h.readOffer(w, r, name)
	if err != nil {
		// Else net/http waits for the rest of the body (of a
		// Content-Length) before the answer, until the deadline.
		w.Header().Set("Connection", "close")
		if now, ok := h.Account(key); !ok || now != name { // EndAccount stopped the read
			unauthorized(w)
		} else {
			http.Error(w, err.Error(), status)
		}
		return
	}
	voice, err := h.voiceFor(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	o, err := checkOffer(string(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The key again (it may have changed while the offer came), under the
	// lock EndAccount takes: a change after this ends the session.
	s, old, err := h.reserve(name, key)
	for _, o := range old {
		o.close()
	}
	if errors.Is(err, errKeyChanged) {
		unauthorized(w)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	answer, err := s.open(o, string(body), voice)
	if err != nil {
		slog.Warn("stream from OBS: no WebRTC session", "account", name, "err", err)
		s.close()
		http.Error(w, "cannot set up the WebRTC session", http.StatusBadRequest)
		return
	}
	slog.Info("stream from OBS", "account", name)
	go s.run()
	w.Header().Set("Content-Type", "application/sdp")
	w.Header().Set("Location", Path+"/"+s.id)
	// Not chunked: FFmpeg's WHIP client reads the end of a chunked body as an error.
	w.Header().Set("Content-Length", strconv.Itoa(len(answer)))
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, answer)
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "unknown stream key (see Settings in Kallisti)", http.StatusUnauthorized)
}

var errKeyChanged = errors.New("the stream key changed")

// readOffer reads the offer within OfferTimeout (set by start), if
// fewer than MaxSessions are coming; EndAccount (a new key) stops it. The
// error goes to the encoder with the status.
func (h *Handler) readOffer(w http.ResponseWriter, r *http.Request, account string) ([]byte, int, error) {
	rc := http.NewResponseController(w)
	h.mu.Lock()
	if h.closed || len(h.pending) >= h.MaxSessions {
		h.mu.Unlock()
		return nil, http.StatusServiceUnavailable, errBusy
	}
	if h.pending == nil {
		h.pending = map[*http.ResponseController]string{}
	}
	h.pending[rc] = account
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, rc)
		h.mu.Unlock()
	}()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxOffer))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, http.StatusRequestEntityTooLarge, errors.New("the offer is too large")
		}
		return nil, http.StatusRequestTimeout, errors.New("the offer did not come in time")
	}
	rc.SetReadDeadline(time.Time{})
	return body, 0, nil
}

var errBusy = errors.New("too many streams from OBS at once, try again later")

// reserve makes a session (not yet open) if there is room and the key is
// still valid. It replaces the account's old ones (OBS reconnecting),
// returned to be closed.
func (h *Handler) reserve(account, key string) (*session, []*session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name, ok := h.Account(key); !ok || name != account {
		return nil, nil, errKeyChanged
	}
	old := h.takeAccountLocked(account)
	if h.sessions == nil {
		h.sessions = map[string]*session{}
	}
	if h.closed || len(h.sessions) >= h.MaxSessions {
		return nil, old, errBusy
	}
	b := make([]byte, 16)
	rand.Read(b)
	s := newSession(h, hex.EncodeToString(b), account)
	h.sessions[s.id] = s
	return s, old, nil
}

// take removes a session from the list (to close it).
func (h *Handler) take(id string) *session {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[id]
	delete(h.sessions, id)
	return s
}

func (h *Handler) takeAccount(name string) []*session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.takeAccountLocked(name)
}

func (h *Handler) takeAccountLocked(name string) []*session {
	var out []*session
	for id, s := range h.sessions {
		if s.account == name {
			out = append(out, s)
			delete(h.sessions, id)
		}
	}
	return out
}

// EndAccount ends an account's stream and stops reading its offers (its
// key changed).
func (h *Handler) EndAccount(name string) {
	h.mu.Lock()
	for rc, account := range h.pending {
		if account == name {
			rc.SetReadDeadline(time.Now())
		}
	}
	h.mu.Unlock()
	for _, s := range h.takeAccount(name) {
		s.close()
	}
}

// Close ends every stream.
func (h *Handler) Close() {
	h.mu.Lock()
	all := h.sessions
	h.sessions, h.closed = nil, true
	h.mu.Unlock()
	for _, s := range all {
		s.close()
	}
	if h.Closer != nil {
		h.Closer.Close()
	}
}

// Sessions: how many streams are open (or opening).
func (h *Handler) Sessions() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

// api is pion for one offer: its own codecs only, as offered.
func (h *Handler) api(o offer) (*webrtc.API, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(o.video, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}
	if o.audio != nil {
		if err := m.RegisterCodec(*o.audio, webrtc.RTPCodecTypeAudio); err != nil {
			return nil, err
		}
	}
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, err
	}
	return webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i), webrtc.WithSettingEngine(h.Settings)), nil
}

// voiceFor is the account's voice session, if it may stream there now (a
// session can also be in text only).
func (h *Handler) voiceFor(name string) (uint32, error) {
	voice, ok := h.Session(name)
	if !ok {
		return 0, errors.New("connect to Kallisti and join a voice channel first")
	}
	return voice, h.Hub.MayStream(voice)
}
