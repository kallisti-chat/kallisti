// Package bridge moves raw bytes between browser WebSockets and a Mumble
// server: a session of the built-in one, or the TLS port of Murmur. Mumble
// framing is done by the browser, so chunk boundaries do not matter here.
package bridge

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"google.golang.org/protobuf/proto"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"kallisti.chat/server/internal/clientip"
	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/sidecar"
)

// MaxPayload is the Mumble frame limit. Larger messages close with 1009.
const MaxPayload = 8 * 1024 * 1024

// maxTextFrame limits sidecar requests: the largest send fits (a
// 2000-character message with 10 files' descriptions, see the test).
const maxTextFrame = 64 * 1024

type Options struct {
	AllowedOrigins    []string
	MaxConnections    int
	MaxPerIP          int
	TrustForwardedFor bool
	// Dial opens the upstream connection: TLS to Murmur, or a session of
	// the built-in Mumble server.
	Dial func(ctx context.Context, up Upstream) (net.Conn, error)
	// Account returns the account behind a request (session cookie). Nil:
	// everyone is a guest.
	Account func(r *http.Request) (Account, bool)
	// IsAccountName: guests may not use the names of accounts.
	IsAccountName func(name string) bool
	// Sidecar serves text channels over text frames. Nil: text frames close
	// the connection with 1003.
	Sidecar *sidecar.Hub
	// Refuse a guest whose name another web guest already uses. For Murmur:
	// all web users share our IP there, and it kicks the older session of a
	// name from the same IP.
	UniqueGuestNames bool
	// KeepAlive: how often the bridge pings the Mumble server for each web
	// client, and the browser (WebSocket ping). Browsers slow page timers in
	// background tabs, so the client's own pings may come too late. 0: 10
	// seconds.
	KeepAlive time.Duration
	// PingGrace: a browser without a pong for this long is closed (at the
	// next failed ping). Long, so a page the system suspends for a while (a
	// macOS app in the background, Android) keeps its session. 0: 2 minutes.
	PingGrace time.Duration
	// TokenEnded: the request brings a session token (not a cookie) that is
	// no session any more (see auth.Service.TokenEnded).
	TokenEnded func(r *http.Request) bool
}

// Upstream is who a connection is for.
type Upstream struct {
	IP          string           // the client's address
	AccountName string           // "" for guests
	Cert        *tls.Certificate // the account's client certificate, or nil
}

// Account is the account behind a connection.
type Account struct {
	Name    string
	Cert    *tls.Certificate // its client certificate
	ID      int64            // for Revoke
	Session string           // the session's key, for Revoke
}

type Bridge struct {
	opts     Options
	mu       sync.Mutex
	active   int
	perIP    map[string]int
	guests   map[string]bool             // names.Key of connected guests
	accounts map[*websocket.Conn]Account // open account connections
	revokes  uint64                      // Revoke calls, for lookup
	closed   bool
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func New(opts Options) *Bridge {
	ctx, cancel := context.WithCancel(context.Background())
	return &Bridge{opts: opts, ctx: ctx, cancel: cancel, perIP: map[string]int{}, guests: map[string]bool{},
		accounts: map[*websocket.Conn]Account{}}
}

// Revoke closes the open account connections (code 1008) whose account ID
// and session match: their sessions ended. The web client knows the reason
// text and does not reconnect.
func (b *Bridge) Revoke(match func(accountID int64, session string) bool) {
	// match may ask the database: not under the lock. Connections listed
	// later look up their account again (see lookup).
	b.mu.Lock()
	b.revokes++
	open := maps.Clone(b.accounts)
	b.mu.Unlock()
	var ended []*websocket.Conn
	for ws, acc := range open {
		if match(acc.ID, acc.Session) {
			ended = append(ended, ws)
		}
	}
	// Counted like connections (see admit). After Close, shutdown ends them.
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for _, ws := range ended {
		// Close waits for the browser's answer: not one after another.
		b.wg.Go(func() { ws.Close(websocket.StatusPolicyViolation, "logged out") })
	}
}

// lookup returns the request's account and lists its connection for
// Revoke. A Revoke during the lookup may have missed it: then look again.
func (b *Bridge) lookup(ws *websocket.Conn, r *http.Request) (Account, bool) {
	if b.opts.Account == nil {
		return Account{}, false
	}
	for {
		b.mu.Lock()
		gen := b.revokes
		b.mu.Unlock()
		acc, ok := b.opts.Account(r)
		b.mu.Lock()
		if b.revokes == gen {
			if ok {
				b.accounts[ws] = acc
			}
			b.mu.Unlock()
			return acc, ok
		}
		b.mu.Unlock()
	}
}

// Close refuses new connections, ends the open ones (close code 1001) and
// waits for them.
func (b *Bridge) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.cancel()
	b.wg.Wait()
}

// admit counts a new connection, or returns the HTTP status that refuses
// it. Under the lock, so wg.Add never runs concurrently with Close's wg.Wait.
func (b *Bridge) admit(ip string) int {
	ip = clientip.Key(ip)
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.closed || b.active >= b.opts.MaxConnections:
		return http.StatusServiceUnavailable
	case b.perIP[ip] >= b.opts.MaxPerIP:
		return http.StatusTooManyRequests
	}
	b.active++
	b.perIP[ip]++
	b.wg.Add(1)
	return 0
}

func (b *Bridge) release(ip string) {
	ip = clientip.Key(ip)
	b.mu.Lock()
	b.active--
	if b.perIP[ip]--; b.perIP[ip] <= 0 {
		delete(b.perIP, ip)
	}
	b.mu.Unlock()
	b.wg.Done()
}

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !config.OriginAllowed(b.opts.AllowedOrigins, r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	ip := clientip.Of(r, b.opts.TrustForwardedFor)
	if status := b.admit(ip); status != 0 {
		http.Error(w, "too many connections", status)
		return
	}
	defer b.release(ip)

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The origin is checked above, against our own list.
		InsecureSkipVerify: true,
		// Compression adds latency to voice.
		CompressionMode: websocket.CompressionDisabled,
		// The desktop app offers "kallisti" next to its session token (see
		// auth.SessionProtocol); browsers need one of them chosen.
		Subprotocols: []string{"kallisti"},
	})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(MaxPayload)

	acc, isAccount := b.lookup(ws, r)
	// A token that is no session (any more): the client asks for a login.
	if !isAccount && b.opts.TokenEnded != nil && b.opts.TokenEnded(r) {
		ws.Close(websocket.StatusPolicyViolation, "session expired")
		return
	}
	// Guests only on our own pages and in the apps: else any site could
	// make its visitors join (other sites connect with an account's token).
	if !isAccount && !config.OwnSite(b.opts.AllowedOrigins, r.Header.Get("Origin")) {
		ws.Close(websocket.StatusPolicyViolation, "join as a guest on the server's own site")
		return
	}
	if isAccount {
		defer func() {
			b.mu.Lock()
			delete(b.accounts, ws)
			b.mu.Unlock()
		}()
	}

	// Read up to Authenticate first. Accounts must use their own name (their
	// certificate is their registration). Guests may not use an account's
	// name, nor a name another web guest has.
	var hello []byte
	if b.opts.UniqueGuestNames || isAccount {
		raw, early, ok := b.readHello(ws)
		if !ok {
			return
		}
		// Compared the way Murmur sees names: trimmed, NFC, without case.
		name := names.Key(raw)
		reject := func(typ pb.Reject_RejectType, reason string) {
			ws.Write(b.ctx, websocket.MessageBinary, mumble.RejectFrame(typ, reason))
			ws.Close(websocket.StatusNormalClosure, "rejected")
		}
		switch {
		case isAccount && name != names.Key(acc.Name):
			reject(pb.Reject_InvalidUsername, "Use your account name, "+acc.Name+".")
			return
		case !isAccount && strings.Contains(name, "@"):
			// name@host: an account from another server's (see names.Remote).
			reject(pb.Reject_InvalidUsername, "Guests take names without @.")
			return
		case !isAccount && b.opts.IsAccountName != nil && b.opts.IsAccountName(name):
			reject(pb.Reject_UsernameInUse, "This name belongs to an account. Log in to use it.")
			return
		case !isAccount && b.opts.UniqueGuestNames:
			if !b.reserveName(name) {
				reject(pb.Reject_UsernameInUse, "This name is already in use.")
				return
			}
			defer b.releaseName(name)
		}
		hello = early
	}

	up := Upstream{IP: ip}
	if isAccount {
		up.AccountName, up.Cert = acc.Name, acc.Cert
	}
	upstream, err := b.opts.Dial(b.ctx, up)
	if err != nil {
		// Cancelled by a shutdown: as for open connections (see below).
		if b.ctx.Err() != nil {
			ws.Close(websocket.StatusGoingAway, "server shutting down")
			return
		}
		slog.Warn("upstream connect failed", "err", err)
		ws.Close(websocket.StatusInternalError, "upstream unavailable")
		return
	}
	defer upstream.Close()
	if _, err := upstream.Write(hello); err != nil {
		ws.Close(websocket.StatusInternalError, "upstream write failed")
		return
	}

	// Not derived from b.ctx: cancelling a read drops the connection without
	// a close frame. Shutdown goes through the AfterFunc below instead.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &conn{bridge: b, ws: ws, upstream: upstream, ctx: ctx, cancel: cancel}

	// Shutdown: close the WebSocket with 1001 and stop the upstream reader.
	stop := context.AfterFunc(b.ctx, func() {
		ws.Close(websocket.StatusGoingAway, "server shutting down")
		upstream.Close()
	})
	defer stop()

	var wg sync.WaitGroup
	if b.opts.Sidecar != nil {
		c.side = b.opts.Sidecar.Connect()
		wg.Go(c.writeSidecar)
	}
	wg.Go(c.upstreamToClient)
	wg.Go(c.keepAlive)
	c.clientToUpstream()
	// Stop everything else and wait for it, so no goroutine outlives the
	// connection count.
	cancel()
	upstream.Close()
	if c.side != nil {
		c.side.Close()
	}
	wg.Wait()
}

const (
	defaultKeepAlive = 10 * time.Second
	defaultPingGrace = 2 * time.Minute
)

// keepAlive pings upstream (the Mumble server drops silent clients after
// 30 s) and the browser (its network code answers, not the page), until
// the connection ends. A browser without a pong for the grace is closed.
//
// One browser ping at a time waits for its pong up to the end of the
// grace, in its own goroutine, so the loop keeps pinging upstream. While a
// page does not read, the writes to it block
// (voice and Mumble state wait upstream, text overflows to a resync) and
// pings wait for them. The write of a ping frame itself may time out (5 s
// in the websocket library) and close the connection: only if the socket
// fills up exactly then.
func (c *conn) keepAlive() {
	every := cmp.Or(c.bridge.opts.KeepAlive, defaultKeepAlive)
	grace := cmp.Or(c.bridge.opts.PingGrace, defaultPingGrace)
	tick := time.NewTicker(every)
	defer tick.Stop()
	lastPong := time.Now()
	// A shorter wait than the grace would end a slow browser early: the
	// library closes the connection when a write's context ends mid-frame.
	pong := make(chan error, 1)
	pinging := false
	defer func() {
		if pinging {
			<-pong // ends with c.ctx: no goroutine outlives the connection
		}
	}()
	for {
		select {
		case <-c.ctx.Done():
			return
		case err := <-pong:
			pinging = false
			switch {
			case err == nil:
				lastPong = time.Now()
			case c.ctx.Err() != nil:
				return
			case time.Since(lastPong) >= grace:
				// No close handshake: the browser does not answer anyway.
				c.ws.CloseNow()
				c.upstream.Close()
				return
			}
			continue
		case <-tick.C:
		}
		ping := mumble.MessageFrame(mumble.TypePing, &pb.Ping{Timestamp: proto.Uint64(uint64(time.Now().UnixMilli()))})
		if _, err := c.upstream.Write(ping); err != nil {
			return
		}
		if !pinging {
			pinging = true
			wait := max(grace-time.Since(lastPong), every)
			go func() {
				ctx, cancel := context.WithTimeout(c.ctx, wait)
				defer cancel()
				pong <- c.ws.Ping(ctx)
			}()
		}
	}
}

func (b *Bridge) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active
}

// conn is one browser connection with its upstream.
type conn struct {
	bridge   *Bridge
	ws       *websocket.Conn
	upstream net.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	side     *sidecar.Conn // nil without sidecar
	sniffer  mumble.Sniffer
	sniffErr bool
}

// Blocking writes give natural backpressure: a slow side stops the reader
// of the other side.
func (c *conn) upstreamToClient() {
	defer c.cancel()
	buf := make([]byte, 32*1024)
	for {
		n, err := c.upstream.Read(buf)
		if n > 0 {
			if werr := c.ws.Write(c.ctx, websocket.MessageBinary, buf[:n]); werr != nil {
				return
			}
			c.sniff(buf[:n])
		}
		if err != nil {
			// Ended by us (client gone, shutdown): nothing to tell.
			if c.ctx.Err() != nil || c.bridge.ctx.Err() != nil {
				return
			}
			if errors.Is(err, io.EOF) {
				c.ws.Close(websocket.StatusNormalClosure, "")
			} else {
				c.ws.Close(websocket.StatusInternalError, "upstream error")
			}
			return
		}
	}
}

// sniff follows the stream (after forwarding it, so voice is not delayed)
// to learn the Mumble identity for the sidecar.
func (c *conn) sniff(chunk []byte) {
	if c.side == nil || c.sniffErr {
		return
	}
	id, changed, err := c.sniffer.Feed(chunk)
	if err != nil {
		slog.Warn("cannot follow the Mumble stream, text channels disabled for this connection", "err", err)
		c.sniffErr = true
		c.side.Fail("text channels are not available on this connection")
		return
	}
	if changed {
		c.side.SetIdentity(id)
	}
}

func (c *conn) clientToUpstream() {
	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			if c.side == nil {
				c.ws.Close(websocket.StatusUnsupportedData, "binary frames only")
				return
			}
			if len(data) > maxTextFrame {
				c.ws.Close(websocket.StatusMessageTooBig, "request too large")
				return
			}
			c.side.Handle(data)
			continue
		}
		if _, err := c.upstream.Write(data); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				c.ws.Close(websocket.StatusInternalError, "upstream write failed")
			}
			return
		}
	}
}

// writeSidecar sends the sidecar's messages as text frames. It ends when
// the sidecar connection closes its outbox.
func (c *conn) writeSidecar() {
	for msg := range c.side.Outbox() {
		if c.ws.Write(c.ctx, websocket.MessageText, msg) != nil {
			return
		}
	}
}

// helloTimeout: a client that does not authenticate in time is dropped.
const helloTimeout = 10 * time.Second

// maxGreeting limits all bytes before Authenticate (the web client sends
// Version and Authenticate, a few hundred bytes).
const maxGreeting = 64 * 1024

// readHello reads the client's first binary frames up to Authenticate. It
// returns the requested name and all bytes read (to forward unchanged).
func (b *Bridge) readHello(ws *websocket.Conn) (string, []byte, bool) {
	ctx, cancel := context.WithTimeout(b.ctx, helloTimeout)
	defer cancel()
	var auth mumble.AuthReader
	var early []byte
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			ws.Close(websocket.StatusPolicyViolation, "no authentication")
			return "", nil, false
		}
		if typ != websocket.MessageBinary {
			continue // sidecar requests before the login: nothing to answer yet
		}
		if len(early)+len(data) > maxGreeting {
			ws.Close(websocket.StatusPolicyViolation, "greeting too large")
			return "", nil, false
		}
		early = append(early, data...)
		name, done, err := auth.Feed(data)
		if err != nil {
			ws.Close(websocket.StatusProtocolError, "invalid Mumble stream")
			return "", nil, false
		}
		if done {
			return name, early, true
		}
	}
}

// reserveName and releaseName take names.Key forms.
func (b *Bridge) reserveName(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.guests[key] {
		return false
	}
	b.guests[key] = true
	return true
}

func (b *Bridge) releaseName(key string) {
	b.mu.Lock()
	delete(b.guests, key)
	b.mu.Unlock()
}
