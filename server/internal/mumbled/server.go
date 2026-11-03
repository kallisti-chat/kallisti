// Package mumbled is the built-in Mumble server. The state (channels,
// users) is in State, without IO; this file has the connections, voice and
// UDP, messages.go the control messages. Only clients from 1.5 on (the
// protobuf UDP format) are accepted for now.
package mumbled

import (
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/cryptstate"
	"kallisti.chat/server/internal/homes"
	"kallisti.chat/server/internal/msgrules"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/recording"
)

type Config struct {
	Welcome  string
	RootName string // default "Root"
	Password string // for guests; registered users need none
	MaxUsers int    // default 100
	MaxPerIP int    // connections per IP (IPv6: per /64); 0: no limit
	// ReservedName reports names (as names.Key) that belong to Kallisti
	// accounts: nobody else may use them. Nil: none.
	ReservedName func(key string) bool
	// AllowGuest checks admission for an unregistered connection. Registered
	// identities and existing web accounts are checked first. Nil: open.
	AllowGuest func() bool
	// WebAccountExists: the Kallisti account with this name and certificate
	// still exists (a guest account can be deleted while it connects).
	// Nil: yes.
	WebAccountExists func(name, certHash string) bool
	// AccountLogin checks a Kallisti account's password, for Mumble clients
	// that log in with it. It returns the account, or nil if the name has
	// no account with a password; ok false: wrong password. Nil: none.
	AccountLogin func(name, password string) (a *WebAccount, ok bool)
	// OnChannelText gets every text a client sends to a channel (as plain
	// text), for example to store it. It runs outside the server's lock.
	OnChannelText func(ChannelText)
	// CheckChannelText may refuse a client's text to a channel it writes to
	// directly (the error is the refusal it gets), before anyone gets it.
	// It runs outside the server's lock. Nil: none is refused.
	CheckChannelText func(session, channel uint32) error
	// OnPrivateText gets private messages between registered users (once
	// per recipient), to store them; web recipients get them from there,
	// not from this server. It runs outside the server's lock.
	OnPrivateText func(PrivateText)
	// GuestAccount: the Kallisti account with this name is a guest account
	// (see SetGuestsReadOnly). Nil: none is.
	GuestAccount func(name string) bool
	// OnGuestsReadOnlyChanged runs after an admin changed the setting.
	OnGuestsReadOnlyChanged func()
	// OnRolesChanged runs after admins or moderators changed (see Roles).
	OnRolesChanged func()
	// OnUserChanged hears registrations made or renamed (name), or removed
	// (name ""), in order, outside the server lock (see Users).
	OnUserChanged func(id uint32, name string)
	// OnPictureChanged hears a registered user's saved avatar set or
	// removed (picture: there is one Avatars serves), in order, outside
	// the server lock (see Pictures).
	OnPictureChanged func(id uint32, picture bool)
	// OnListenOnlyChanged runs after a channel's ACL changed (see
	// ListenOnly).
	OnListenOnlyChanged func()
	// OnPrivateChanged runs after a channel's ACL changed (see Private).
	OnPrivateChanged func()
	// OnTimeoutsChanged runs after a time-out began or ended (see
	// Timeouts).
	OnTimeoutsChanged func()
	// OnUserMoved hears users join (from NoChannel), change channels and
	// leave (to NoChannel), in order, outside the server lock. Text-only
	// sessions are in no channel (see textonly.go): they join and leave
	// from NoChannel to NoChannel, and take or leave voice like a join or
	// a leave.
	OnUserMoved func(session, from, to uint32)
	// TextOverflows is the number of sidecar messages each session lost
	// (see ConnectionStats). Nil: none.
	TextOverflows func() map[uint32]uint64
	// Recordings keeps channel recordings (nil: no recording).
	// OnRecordingChanged runs after one started or stopped (see
	// Recording), outside the server lock.
	Recordings         *recording.Library
	OnRecordingChanged func()
	DB                 *DB // nil: nothing is saved
}

const (
	handshakeTimeout = 10 * time.Second
	// Mumble clients ping every 5 s; Murmur drops them after 30 s.
	idleTimeout  = 30 * time.Second
	writeTimeout = 10 * time.Second
	// The web bridge keeps a suspended page up to two minutes
	// (bridge.Options.PingGrace) and stops reading from us meanwhile. It
	// closes the pipe when it gives up. Voice is dropped and other frames
	// queue (a full queue still disconnects).
	webWriteTimeout = 3 * time.Minute
	// When a connection ends, queued frames (a kick reason) still go out.
	flushTimeout = 2 * time.Second
	// Before Authenticate frames stay small; after it, nothing in the
	// spike needs more (no avatars yet).
	maxHandshakeFrame = 64 << 10
	maxFrame          = 128 << 10
	queueSize         = 512
	maxTextLength     = 5000
	// The CELT version Murmur announces (0.7.0) next to Opus.
	celt070 = -2147483637
	// Murmur's default messagelimit and messageburst: text messages and
	// state changes per second.
	messageLimit = 1
	messageBurst = 5
)

var (
	versionV1 = uint32(1<<16 | 5<<8)
	versionV2 = uint64(1<<48 | 5<<32)
	// As Murmur: ask the client for a new nonce when its UDP packets did
	// not decrypt for this long, and not more often. A variable for tests.
	resyncAfter = 5 * time.Second
)

// ChannelText is a text a client sent to a channel (see
// Config.OnChannelText).
type ChannelText struct {
	From       uint32 // session
	Name       string
	Registered bool
	UserID     uint32 // with Registered
	Channel    uint32
	Text       string
	Images     []Image
}

// PrivateText is a private message between two registered users (see
// Config.OnPrivateText), as plain text.
type PrivateText struct {
	From   uint32 // session
	Name   string
	UserID uint32
	To     uint32 // the recipient's user id
	Text   string
	Images []Image
	// One of the recipient's Mumble clients got it (then it is read).
	Delivered bool
}

type Server struct {
	cfg Config
	// Screen share sound, by streamer session (see streamsound.go). Under mu.
	streams map[uint32]*streamUser
	// Taken before mu by changes that are saved, so saves keep their order.
	saveMu sync.Mutex
	// Web account registrations and their removal take turns (see
	// Unregister).
	regMu sync.Mutex

	logins logins
	conns  conns
	pings  *rate.Limiter

	sessions sync.WaitGroup // running connections, for Close
	closing  atomic.Bool
	// Hooks that must not run under mu go through this queue, in order.
	events     *eventQueue
	textParent uint32 // the text channel mirrors' parent (see SyncTextChannels), under mu

	// Read at every accept: not under mu, which the voice path uses.
	bans atomic.Pointer[[]BanEntry]

	mu      sync.Mutex
	state   *State
	clients map[uint32]*client
	byHost  map[netip.Addr]map[*client]bool // for UDP from new addresses
	byAddr  map[netip.AddrPort]*client
	next    uint32
	udp     *net.UDPConn
	// Time-outs by user (see timeouts.go). Under mu.
	timeouts map[timeoutKey]*timeout
	// Snapshots of the connections (see watch.go).
	watch watch
	// Message rules, and limited users' last messages (see
	// messagerules.go). Under mu.
	rules    msgrules.Rules
	homes    homes.Rules // see homes.go
	lastPost map[timeoutKey]time.Time
	now      func() time.Time
	pick     func(n int) int // a random number below n: word replacements
	// Running recordings by channel (see recording.go). Under mu.
	// recordingEnds: those of removed channels, ending.
	recordings    map[uint32]*recording.Recording
	recordingEnds sync.WaitGroup
}

type client struct {
	conn      net.Conn
	closeOnce sync.Once
	out       chan []byte
	voice     chan []byte // voice frames: sent before out (see writer)
	session   uint32
	name      string
	host      netip.Addr
	legacy    bool // a client before 1.5: legacy UDP format
	web       bool // a web client through the bridge (ServeConn), not a Mumble client
	opus      bool
	// What it said about itself, and its certificates (for UserStats).
	version                uint64
	release, os, osVersion string
	certs                  [][]byte

	limiter   *rate.Limiter
	bandwidth *rate.Limiter // voice bytes per second
	// VoiceTarget messages: own limit, whisper keys send them often.
	targetLimiter *rate.Limiter

	// Guarded by Server.mu.
	udpAddr  netip.AddrPort
	udpVoice bool            // send voice over UDP (it sent voice over UDP)
	seen     map[uint32]bool // channels it was told about (see visibility.go)

	cryptMu     sync.Mutex
	crypt       *cryptstate.CryptState
	lastGood    time.Time
	lastRequest time.Time
	resyncs     uint32

	stats   connStats     // see stats.go
	dropped atomic.Uint64 // voice frames dropped from a full queue
}

func New(cfg Config) (*Server, error) {
	var saved []Channel
	var lastID uint32
	if cfg.DB != nil {
		// An admin's name wins over the configured one.
		name, err := cfg.DB.ServerName()
		if err != nil {
			return nil, err
		}
		cfg.RootName = cmp.Or(name, cfg.RootName)
		if saved, err = cfg.DB.Channels(); err != nil {
			return nil, err
		}
		if lastID, err = cfg.DB.NextChannel(); err != nil {
			return nil, err
		}
	}
	cfg.RootName = cmp.Or(cfg.RootName, "Root")
	var bans []BanEntry
	if cfg.DB != nil {
		var err error
		if bans, err = cfg.DB.Bans(); err != nil {
			return nil, err
		}
	}
	state := NewState(cfg.RootName, saved)
	state.SetLastChannelID(lastID)
	var rules msgrules.Rules
	var homeRules homes.Rules
	if cfg.DB != nil {
		var err error
		if _, err = cfg.DB.setting("home_servers", &homeRules); err != nil {
			return nil, err
		}
		if state.GuestsReadOnly, err = cfg.DB.GuestsReadOnly(); err != nil {
			return nil, err
		}
		if rules, err = cfg.DB.MessageRules(); err != nil {
			return nil, err
		}
		if state.Captcha, err = cfg.DB.meta("captcha"); err != nil {
			return nil, err
		}
		state.Captcha = min(state.Captcha, uint32(len(CaptchaLevels)-1))
	} else {
		state.Channels[0].ACLs, state.Channels[0].Groups = DefaultRootACL()
	}
	srv := &Server{
		cfg:      cfg,
		state:    state,
		clients:  map[uint32]*client{},
		byAddr:   map[netip.AddrPort]*client{},
		byHost:   map[netip.Addr]map[*client]bool{},
		streams:  map[uint32]*streamUser{},
		rules:    rules,
		homes:    homeRules,
		lastPost: map[timeoutKey]time.Time{},
		now:      time.Now,
		pick:     rand.IntN,
	}
	srv.bans.Store(&bans)
	if err := srv.loadTimeouts(); err != nil {
		return nil, err
	}
	srv.pings = rate.NewLimiter(pingsPerSecond, pingsPerSecond)
	if cfg.OnUserMoved != nil || cfg.OnUserChanged != nil || cfg.OnPictureChanged != nil {
		srv.events = newEventQueue()
	}
	state.TimedOutIn = func(u *User, channel uint32) bool {
		_, ok := srv.timedOutInLocked(u, channel)
		return ok
	}
	state.Mirror = srv.inTextTree
	state.OnMove = func(session, from, to uint32) {
		if from != NoChannel { // a join is told after ServerSync (see join)
			srv.tellRecordingLocked(session, to)
		}
		if cfg.OnUserMoved != nil {
			from, to = outside(from), outside(to)
			srv.events.add(func() { cfg.OnUserMoved(session, from, to) })
		}
	}
	return srv, nil
}

// Serve accepts TLS connections until the listener closes. Other errors
// (out of file descriptors) wait and try again, as net/http does.
func (s *Server) Serve(ln net.Listener) error {
	var wait time.Duration
	for {
		conn, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return err
		}
		if err != nil {
			wait = min(max(2*wait, 5*time.Millisecond), time.Second)
			slog.Warn("mumble: accept", "err", err, "retry", wait)
			time.Sleep(wait)
			continue
		}
		wait = 0
		// Banned addresses are dropped before TLS, as in Murmur.
		host := remoteHost(conn)
		if s.banned(host, "") != nil || !s.conns.add(host, s.cfg.MaxPerIP) {
			conn.Close()
			continue
		}
		if !s.track() {
			s.conns.remove(host, true)
			conn.Close()
			continue
		}
		go func() {
			defer s.sessions.Done()
			s.handle(conn, Origin{Host: host}, true)
		}()
	}
}

// track counts a new connection, unless the server is closing.
func (s *Server) track() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing.Load() {
		return false
	}
	s.sessions.Add(1)
	return true
}

// Close disconnects all clients and waits (briefly) until their sessions
// ended and their leaves were reported, so last channels are saved before
// the database closes.
func (s *Server) Close() {
	s.mu.Lock()
	s.closing.Store(true)
	clients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
	s.stopAllRecordings()
	s.stopTimeouts()
	done := make(chan struct{})
	go func() {
		s.sessions.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	if s.events != nil {
		s.events.close(flushTimeout)
	}
}

func readFrame(conn net.Conn, limit int) (uint16, []byte, error) {
	var header [6]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(header[2:])
	if n > uint32(limit) {
		return 0, nil, errors.New("frame too large")
	}
	body := make([]byte, n)
	_, err := io.ReadFull(conn, body)
	return binary.BigEndian.Uint16(header[:]), body, err
}

// close never blocks: closing a TLS connection waits for a writer that
// may be stuck, and callers hold locks.
func (c *client) close() {
	c.closeOnce.Do(func() { go c.conn.Close() })
}

// send queues a frame. A full queue drops voice; for anything else the
// client is too slow and is disconnected.
func (c *client) send(frame []byte, voice bool) {
	if voice {
		select {
		case c.voice <- frame:
		default:
			c.dropped.Add(1)
		}
		return
	}
	select {
	case c.out <- frame:
	default:
		c.close()
	}
}

// closeAfterFlush closes the connection once the queued frames are sent.
func (c *client) closeAfterFlush() {
	select {
	case c.out <- nil:
	default:
		c.close()
	}
}

func (c *client) sendMsg(typ uint16, m proto.Message) {
	c.send(mumble.MessageFrame(typ, m), false)
}

// writer sends the queues until closeAfterFlush or a write error. Voice
// goes first: queued avatars or a big state would hold it back for
// seconds on a slow link. As over UDP, it may come before the state that
// introduces its speaker, even before ServerSync (clients wait for that).
// On a link too slow for the voice, the other queue fills and the client
// is dropped, as before.
func (c *client) writer() {
	for {
		var b []byte
		select {
		case b = <-c.voice:
		default:
			select {
			case b = <-c.voice:
			case b = <-c.out:
			}
		}
		if b == nil { // closeAfterFlush
			c.close()
			return
		}
		c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout()))
		if _, err := c.conn.Write(b); err != nil {
			c.close()
			return
		}
	}
}

func (c *client) writeTimeout() time.Duration {
	if c.web {
		return webWriteTimeout
	}
	return writeTimeout
}

// cryptStats counts the UDP packets from the client.
func (c *client) cryptStats() VoiceStats {
	c.cryptMu.Lock()
	defer c.cryptMu.Unlock()
	return VoiceStats{Good: c.crypt.Good, Late: c.crypt.Late, Lost: c.crypt.Lost, Resync: c.resyncs}
}

// banned returns the ban that applies to an address or certificate.
func (s *Server) banned(ip netip.Addr, hash string) *BanEntry {
	now := time.Now()
	for _, b := range s.currentBans() {
		if b.Matches(ip, hash, now) {
			return &b
		}
	}
	return nil
}

// remoteHost is the client's address as bans and limits see it: IPv4 as
// IPv4, without an IPv6 zone (a zone never matches a prefix).
func remoteHost(conn net.Conn) netip.Addr {
	if tcp, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		return tcp.AddrPort().Addr().Unmap().WithZone("")
	}
	return netip.Addr{}
}
