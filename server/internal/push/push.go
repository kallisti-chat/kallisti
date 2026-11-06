// Package push sends Web Push notifications (RFC 8030, with RFC 8291
// encryption and RFC 8292 VAPID through webpush-go) to the browsers of
// registered users, and serves the subscription API.
package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/store"
)

const (
	// One push per user and chat in this time, at most.
	collapseWindow = time.Minute
	// A push the browser did not get in this time is dropped.
	ttl         = 12 * time.Hour
	sendTimeout = 10 * time.Second
	maxBody     = 8 << 10
	maxList     = 1000 // quiet chats, blocked users, loud threads
)

// Note is one push: a short text without the message itself (privacy), the
// page to open, its chat (Parent: a thread's channel) and author. Tag:
// notifications of a chat replace each other. Title, Icon: the server's
// name and logo (see Service.Brand). Allowed: the user may still get it,
// asked again before sending (nil: yes).
type Note struct {
	Channel int64       `json:"-"`
	Parent  int64       `json:"-"`
	Author  string      `json:"-"`
	Allowed func() bool `json:"-"`
	Body    string      `json:"body"`
	URL     string      `json:"url"`
	Tag     string      `json:"tag"`
	Title   string      `json:"title,omitempty"`
	Icon    string      `json:"icon,omitempty"`
}

// Sender delivers an encrypted push; status is the push service's answer.
type Sender interface {
	Send(ctx context.Context, sub store.PushSubscription, payload []byte, topic string) (status int, err error)
}

// Service sends pushes and serves /api/push. Account: the request's
// account and session key.
type Service struct {
	Store     *store.Store
	Sender    Sender
	PublicKey string
	// Brand: the server's name and its logo's icon path ("": Kallisti's;
	// nil: none).
	Brand   func() (title, icon string)
	Now     func() time.Time
	Account func(r *http.Request) (store.Account, string, bool)

	mu   sync.Mutex
	last map[string]time.Time // by user and chat

	// Pushes waiting for a worker (see Start), and those dropped because
	// the queue was full.
	queue   chan job
	dropped atomic.Int64
	ctx     context.Context
	stop    context.CancelFunc
	workers sync.WaitGroup
}

type job struct {
	name string
	note Note
}

// Start runs workers that send queued pushes, at most size waiting.
func (s *Service) Start(workers, size int) {
	s.queue = make(chan job, size)
	s.ctx, s.stop = context.WithCancel(context.Background())
	for range workers {
		s.workers.Go(func() {
			for {
				select {
				case <-s.ctx.Done():
					return
				case j := <-s.queue:
					s.deliver(s.ctx, j.name, j.note)
				}
			}
		})
	}
}

// Stop cancels the sends on their way and waits for the workers; queued
// pushes are dropped.
func (s *Service) Stop() {
	s.stop()
	s.workers.Wait()
}

// due: no push for a user's chat in the window before now (then counted).
func (s *Service) due(user string, channel int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[string]time.Time{}
	}
	key := fmt.Sprintf("%s\x00%d", user, channel)
	if t, ok := s.last[key]; ok && now.Sub(t) < collapseWindow {
		return false
	}
	for k, t := range s.last { // keep the map small
		if now.Sub(t) >= collapseWindow {
			delete(s.last, k)
		}
	}
	s.last[key] = now
	return true
}

// Notify queues a push to a user's browsers (by name): one per user and
// chat in collapseWindow, none when the queue is full or stopped. After
// Start.
func (s *Service) Notify(name string, n Note) {
	if s.ctx == nil || s.ctx.Err() != nil || !s.due(names.Key(name), n.Channel, s.Now()) { // not started, or stopped
		return
	}
	select {
	case s.queue <- job{name, n}:
	default:
		if d := s.dropped.Add(1); d == 1 || d%100 == 0 {
			slog.Warn("push queue full: pushes dropped", "total", d)
		}
	}
}

// quiet: a device keeps the note's chat quiet; a thread also its channel,
// unless it has a setting of its own there.
func quiet(sub store.PushSubscription, n Note) bool {
	return slices.Contains(sub.Quiet, n.Channel) ||
		n.Parent != 0 && slices.Contains(sub.Quiet, n.Parent) && !slices.Contains(sub.Loud, n.Channel)
}

// deliver sends a note to a user's browsers, but not to those that keep
// its chat quiet or blocked its author, and not when the note is no longer
// allowed. Gone subscriptions are dropped.
func (s *Service) deliver(ctx context.Context, name string, n Note) {
	if n.Allowed != nil && !n.Allowed() {
		return
	}
	user, now := names.Key(name), s.Now()
	subs, err := s.Store.PushSubscriptions(user, now)
	if err != nil {
		slog.Error("push subscriptions", "err", err)
		return
	}
	topic := fmt.Sprintf("chat-%d", n.Channel)
	n.Tag = topic
	if s.Brand != nil {
		n.Title, n.Icon = s.Brand()
	}
	payload, _ := json.Marshal(n)
	author := names.Key(n.Author)
	for _, sub := range subs {
		if ctx.Err() != nil {
			return
		}
		if quiet(sub, n) || slices.Contains(sub.Blocked, author) {
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
		status, err := s.Sender.Send(sendCtx, sub, payload, topic)
		cancel()
		switch {
		case err != nil:
			slog.Warn("push", "err", err)
		case status == http.StatusNotFound || status == http.StatusGone:
			if err := s.Store.DropPushEndpoint(sub.Endpoint); err != nil {
				slog.Error("drop a push subscription", "err", err)
			}
		case status >= 300:
			slog.Warn("push refused", "status", status, "service", host(sub.Endpoint))
		}
	}
}

func host(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return u.Host
}

// The push services of the browsers: the server posts only there (a
// subscription must not make it post to any address).
var pushHosts = []string{"fcm.googleapis.com", ".push.services.mozilla.com", ".push.apple.com", ".notify.windows.com"}

func knownPushService(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	return slices.ContainsFunc(pushHosts, func(h string) bool {
		return u.Host == h || strings.HasPrefix(h, ".") && strings.HasSuffix(u.Host, h)
	})
}

type subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Quiet   []int64  `json:"quiet"`
	Blocked []string `json:"blocked"`
	Loud    []int64  `json:"loud"`
}

// validKeys: a P-256 point and a 16-byte secret (RFC 8291), base64url.
func validKeys(p256dh, auth string) bool {
	size := func(s string) int {
		b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
		if err != nil {
			return -1
		}
		return len(b)
	}
	return size(p256dh) == 65 && size(auth) == 16
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ServeHTTP: GET /api/push is the public key; POST and DELETE
// /api/push/subscriptions add (or update) and remove this browser's
// subscription: from its own pages (cookie), or another Kallisti page with
// this server's token (CORS without credentials; see below).
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Other Kallisti pages (several servers in one client) use it with a
	// token: CORS for any origin, never with cookies.
	config.CORS(w, r.Header.Get("Origin"), "GET, POST, DELETE", "Content-Type, Authorization", false)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/api/push" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]string{"publicKey": s.PublicKey})
		return
	}
	if r.URL.Path != "/api/push/subscriptions" || r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.NotFound(w, r)
		return
	}
	// Against CSRF, as in auth: JSON only (forms cannot send it), and not
	// cross-site with cookies (a token is no forgery: pages cannot make
	// browsers add one).
	if ctype, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ctype != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "send JSON")
		return
	}
	if _, token := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); r.Header.Get("Sec-Fetch-Site") == "cross-site" && !token {
		fail(w, http.StatusForbidden, "cross-site requests are not allowed")
		return
	}
	acc, session, ok := s.Account(r)
	if !ok {
		fail(w, http.StatusUnauthorized, "not logged in")
		return
	}
	var sub subscription
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&sub); err != nil {
		fail(w, http.StatusBadRequest, "invalid request")
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.Store.DeletePushSubscription(acc.ID, sub.Endpoint); err != nil {
			fail(w, http.StatusInternalServerError, "cannot remove the subscription")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch {
	case acc.Temporary:
		fail(w, http.StatusForbidden, "push notifications need an account with a password")
	case !knownPushService(sub.Endpoint):
		fail(w, http.StatusBadRequest, "unknown push service")
	case !validKeys(sub.Keys.P256dh, sub.Keys.Auth) || len(sub.Quiet) > maxList || len(sub.Blocked) > maxList || len(sub.Loud) > maxList:
		fail(w, http.StatusBadRequest, "invalid subscription")
	default:
		blocked := make([]string, len(sub.Blocked))
		for i, name := range sub.Blocked {
			blocked[i] = names.Key(name)
		}
		p := store.PushSubscription{Endpoint: sub.Endpoint, P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth, Quiet: sub.Quiet, Blocked: blocked, Loud: sub.Loud}
		if err := s.Store.SetPushSubscription(session, p, s.Now()); err != nil {
			fail(w, http.StatusInternalServerError, "cannot store the subscription")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// Keys are the VAPID keys: the configured ones, else those in the database
// (made on the first start).
func Keys(st *store.Store, public, private string) (string, string, error) {
	if public != "" {
		return public, private, nil
	}
	return st.VAPIDKeys(func() (string, string, error) {
		private, public, err := webpush.GenerateVAPIDKeys()
		return public, private, err
	})
}

// subject is the contact in the VAPID token: the configured one, else the
// server's public HTTPS address, else the project's. webpush-go adds
// "mailto:" itself.
func subject(configured, publicBase string) string {
	switch {
	case configured != "":
		return strings.TrimPrefix(configured, "mailto:")
	case strings.HasPrefix(publicBase, "https://"):
		return publicBase
	}
	return "https://kallisti.chat"
}

// WebPush sends through the browsers' push services.
type WebPush struct {
	publicKey, privateKey, subject string
	client                         *http.Client
}

func NewWebPush(publicKey, privateKey, configuredSubject, publicBase string) *WebPush {
	client := &http.Client{
		Timeout:       sendTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("no redirects") },
	}
	return &WebPush{publicKey, privateKey, subject(configuredSubject, publicBase), client}
}

func (p *WebPush) Send(ctx context.Context, sub store.PushSubscription, payload []byte, topic string) (int, error) {
	res, err := webpush.SendNotificationWithContext(ctx, payload,
		&webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}},
		&webpush.Options{
			HTTPClient: p.client, Subscriber: p.subject, Topic: topic, TTL: int(ttl.Seconds()),
			Urgency: webpush.UrgencyHigh, VAPIDPublicKey: p.publicKey, VAPIDPrivateKey: p.privateKey,
		})
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}
