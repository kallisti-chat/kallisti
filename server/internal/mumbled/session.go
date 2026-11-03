package mumbled

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
	"kallisti.chat/server/internal/cryptstate"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/password"
)

func clientVersion(v *pb.Version) uint64 {
	if v.VersionV2 != nil {
		return v.GetVersionV2()
	}
	v1 := uint64(v.GetVersionV1())
	return (v1>>16)<<48 | (v1>>8&0xff)<<32 | (v1&0xff)<<16
}

func random16() (b [16]byte) {
	rand.Read(b[:])
	return b
}

// handshake reads Version and Authenticate. It returns a Reject frame if
// the client cannot join.
type hello struct {
	name, password string
	tokens         []string
	legacy         bool // before 1.5: legacy UDP format
	noOpus         bool
	version        uint64
	release, os    string
	osVersion      string
}

func handshake(conn net.Conn) (h hello, reject []byte, err error) {
	var version pb.Version
	for {
		typ, body, err := readFrame(conn, maxHandshakeFrame)
		if err != nil {
			return h, nil, err
		}
		switch typ {
		case mumble.TypeVersion:
			proto.Unmarshal(body, &version)
		case mumble.TypeAuthenticate:
			var auth pb.Authenticate
			proto.Unmarshal(body, &auth)
			h = hello{name: strings.TrimSpace(auth.GetUsername()), password: auth.GetPassword(), tokens: auth.Tokens,
				legacy: clientVersion(&version) < versionV2, noOpus: !auth.GetOpus(), version: clientVersion(&version),
				release: version.GetRelease(), os: version.GetOs(), osVersion: version.GetOsVersion()}
			return h, nil, nil
		}
	}
}

// TLSConfig asks clients for their certificate (any certificate: Mumble
// clients have self-signed ones), which identifies registered users.
func TLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS12}
}

// TLSConfigFrom asks get for the certificate on each handshake (for
// certificates that are renewed while the server runs).
func TLSConfigFrom(get func(*tls.ClientHelloInfo) (*tls.Certificate, error)) *tls.Config {
	return &tls.Config{GetCertificate: get, ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS12}
}

// peerChain is the client's certificate chain (DER).
func peerChain(conn net.Conn) (chain [][]byte) {
	if tc, ok := conn.(*tls.Conn); ok {
		for _, cert := range tc.ConnectionState().PeerCertificates {
			chain = append(chain, cert.Raw)
		}
	}
	return chain
}

// certHash is the SHA-1 of the client certificate, as Murmur shows it.
func certHash(chain [][]byte) string {
	if len(chain) == 0 {
		return ""
	}
	sum := sha1.Sum(chain[0])
	return hex.EncodeToString(sum[:])
}

// identity is who a connection is, after authentication.
type identity struct {
	name         string
	registered   bool // the zero value is a guest
	userID       uint32
	hash         string
	tokens       []string
	lastChannel  uint32
	comment      string
	texture      []byte
	newUser      bool // registered just now (a web account's first visit)
	guestAccount bool // see Config.GuestAccount
	since        time.Time
}

// Origin is where a connection comes from, for connections that are not
// accepted by Serve (the Kallisti server's web bridge).
type Origin struct {
	Host    netip.Addr  // the real client address
	Account *WebAccount // a Kallisti account, or nil for a guest
}

// WebAccount is a Kallisti account: a registered user identified by the
// certificate it has (it never connects with TLS itself).
type WebAccount struct {
	Name     string
	CertHash string
}

// ServeConn serves one connection without TLS, for example one end of a
// net.Pipe. Limits per IP are the caller's job.
func (s *Server) ServeConn(conn net.Conn, o Origin) {
	if s.banned(o.Host, "") != nil || !s.track() {
		conn.Close()
		return
	}
	defer s.sessions.Done()
	s.handle(conn, o, false)
}

func (s *Server) identify(h hello, hash string, host netip.Addr, account *WebAccount) (identity, []byte) {
	if s.logins.blocked(host) {
		return identity{}, mumble.RejectFrame(pb.Reject_None, "Too many failed logins. Please try again later.")
	}
	if reject := s.bannedReject(host, hash); reject != nil {
		return identity{}, reject
	}
	if account != nil {
		return s.identifyAccount(h, account)
	}
	var byName, byHash *Registered
	if db := s.cfg.DB; db != nil {
		var err1, err2 error
		byName, err1 = db.UserByName(h.name)
		byHash, err2 = db.UserByHash(hash)
		if err := errors.Join(err1, err2); err != nil {
			slog.Error("looking up a user", "err", err)
			return identity{}, mumble.RejectFrame(pb.Reject_AuthenticatorFail, "Please try again later.")
		}
	}
	wrongPassword := func() (identity, []byte) {
		s.logins.failed(host)
		return identity{}, mumble.RejectFrame(pb.Reject_WrongUserPW, "Wrong certificate or password for existing user.")
	}
	// A Kallisti account's name and password: the account (as on the web,
	// without the server password). Registrations with a password first.
	if s.cfg.AccountLogin != nil && h.password != "" && (byName == nil || byName.PasswordHash == "") {
		a, ok := s.cfg.AccountLogin(h.name, h.password)
		if !ok {
			return wrongPassword()
		}
		if a != nil {
			if reject := s.bannedReject(host, a.CertHash); reject != nil {
				return identity{}, reject
			}
			return s.identifyAccount(h, a)
		}
	}
	r, ok := authenticate(byName, byHash, h.password, hash, checkPassword)
	if !ok {
		return wrongPassword()
	}
	if r != nil {
		comment, texture, err := s.cfg.DB.UserContent(r.ID)
		if err != nil {
			slog.Error("loading a comment", "user", r.ID, "err", err)
		}
		return identity{name: r.Name, registered: true, userID: r.ID, hash: hash, tokens: h.tokens,
			lastChannel: r.LastChannel, comment: comment, texture: texture, since: r.Since}, nil
	}
	if why := names.Check(h.name); why != "" {
		return identity{}, mumble.RejectFrame(pb.Reject_InvalidUsername, why)
	}
	if s.cfg.ReservedName != nil && s.cfg.ReservedName(names.Key(h.name)) {
		return identity{}, mumble.RejectFrame(pb.Reject_UsernameInUse, "This name belongs to an account. Log in to use it.")
	}
	if s.cfg.Password != "" && subtle.ConstantTimeCompare([]byte(h.password), []byte(s.cfg.Password)) != 1 {
		s.logins.failed(host)
		return identity{}, mumble.RejectFrame(pb.Reject_WrongServerPW, "Wrong server password.")
	}
	if s.cfg.AllowGuest != nil && !s.cfg.AllowGuest() {
		return identity{}, mumble.RejectFrame(pb.Reject_AuthenticatorFail, "This server is invite-only. Create an account with an admin's invitation first.")
	}
	return identity{name: h.name, hash: hash, tokens: h.tokens}, nil
}

// bannedReject is the Reject for a banned address or certificate, or nil.
func (s *Server) bannedReject(host netip.Addr, hash string) []byte {
	b := s.banned(host, hash)
	if b == nil {
		return nil
	}
	reason := "You are banned from this server."
	if b.Reason != "" {
		reason = "You are banned from this server: " + b.Reason
	}
	return mumble.RejectFrame(pb.Reject_None, reason)
}

// identifyAccount: a Kallisti account is a registered user, made on its
// first visit. Its name must be the account's.
func (s *Server) identifyAccount(h hello, a *WebAccount) (identity, []byte) {
	if names.Key(h.name) != names.Key(a.Name) {
		return identity{}, mumble.RejectFrame(pb.Reject_InvalidUsername, "Use your account name, "+a.Name+".")
	}
	db := s.cfg.DB
	if db == nil || a.CertHash == "" {
		return identity{}, mumble.RejectFrame(pb.Reject_AuthenticatorFail, "This server does not register users.")
	}
	fail := func(err error) (identity, []byte) {
		slog.Error("registering a web account", "account", a.Name, "err", err)
		return identity{}, mumble.RejectFrame(pb.Reject_AuthenticatorFail, "Please try again later.")
	}
	s.regMu.Lock()
	defer s.regMu.Unlock()
	r, err := db.UserByHash(a.CertHash)
	if err != nil {
		return fail(err)
	}
	newUser := false
	if r == nil && s.cfg.WebAccountExists != nil && !s.cfg.WebAccountExists(a.Name, a.CertHash) {
		return identity{}, mumble.RejectFrame(pb.Reject_AuthenticatorFail, "This guest account has expired. Please join again.")
	}
	if r == nil {
		// A Mumble user with this name but another certificate: an account
		// must not take over a registration (names are kept apart from now
		// on, see Config.ReservedName).
		if other, err := db.UserByName(a.Name); err != nil {
			return fail(err)
		} else if other != nil {
			slog.Warn("a web account's name is registered by a Mumble user", "account", a.Name, "user", other.ID)
			return identity{}, mumble.RejectFrame(pb.Reject_UsernameInUse,
				"A Mumble user has registered this name. Please ask an admin.")
		}
		id, err := db.RegisterUser(a.Name, a.CertHash)
		if err != nil {
			return fail(err)
		}
		r = &Registered{ID: id, Name: a.Name, Since: time.Now()}
		newUser = true
	} else if names.Key(r.Name) != names.Key(a.Name) {
		// An admin renamed the registration: the account keeps its name.
		return identity{}, mumble.RejectFrame(pb.Reject_InvalidUsername,
			"Your Mumble registration is named "+r.Name+" now. Please ask an admin.")
	}
	comment, texture, err := db.UserContent(r.ID)
	if err != nil {
		slog.Error("loading a comment", "user", r.ID, "err", err)
	}
	return identity{name: r.Name, registered: true, userID: r.ID, hash: a.CertHash, tokens: h.tokens,
		lastChannel: r.LastChannel, comment: comment, texture: texture, newUser: newUser, since: r.Since,
		guestAccount: s.cfg.GuestAccount != nil && s.cfg.GuestAccount(a.Name)}, nil
}

// checkPassword runs argon2 with the limit on parallel hashes.
func checkPassword(encoded, pw string) bool {
	defer password.Limit()()
	return password.Check(encoded, pw)
}

// writeReject sends a Reject before the connection closes. The client may
// still be writing (on a net.Pipe that would block both sides): its bytes
// are read and dropped meanwhile.
func writeReject(conn net.Conn, reject []byte) {
	go io.Copy(io.Discard, conn)
	conn.SetWriteDeadline(time.Now().Add(flushTimeout))
	conn.Write(reject)
}

// handle serves a connection. counted: Serve counted it in conns.
func (s *Server) handle(conn net.Conn, o Origin, counted bool) {
	defer conn.Close()
	pending := counted // counted by Serve until it authenticates
	if counted {
		defer func() { s.conns.remove(o.Host, pending) }()
	}
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	h, reject, err := handshake(conn)
	if err != nil {
		return
	}
	host := o.Host
	chain := peerChain(conn)
	hash := certHash(chain)
	if o.Account != nil {
		hash = o.Account.CertHash
	}
	id := identity{}
	if reject == nil {
		id, reject = s.identify(h, hash, host, o.Account)
	}
	if reject != nil {
		writeReject(conn, reject)
		return
	}
	key, clientNonce, serverNonce := random16(), random16(), random16()
	now := time.Now()
	c := &client{
		conn:          conn,
		out:           make(chan []byte, queueSize),
		voice:         make(chan []byte, queueSize),
		name:          id.name,
		limiter:       rate.NewLimiter(messageLimit, messageBurst),
		bandwidth:     rate.NewLimiter(maxBandwidth/8, maxBandwidth/8),
		targetLimiter: rate.NewLimiter(targetLimit, targetBurst),
		crypt:         cryptstate.New(key, serverNonce, clientNonce),
		lastGood:      now,
		host:          host,
		web:           !counted,
		legacy:        h.legacy,
		opus:          !h.noOpus,
		version:       h.version,
		release:       h.release,
		os:            h.os,
		osVersion:     h.osVersion,
		certs:         chain,
		stats:         newConnStats(now),
	}
	setup := &pb.CryptSetup{Key: key[:], ClientNonce: clientNonce[:], ServerNonce: serverNonce[:]}
	reject = s.join(c, id, setup)
	if id.newUser { // after its join (if any): online when told
		s.userChanged(id.userID, id.name)
	}
	if reject != nil {
		writeReject(conn, reject)
		return
	}
	if pending {
		s.conns.authenticated()
		pending = false
	}
	if h.noOpus { // as Murmur: such clients can neither talk nor hear
		c.sendMsg(mumble.TypeTextMessage, &pb.TextMessage{Message: proto.String(
			"Your client does not support the Opus codec: you will not be able to talk or hear. Please update Mumble.")})
	}
	written := make(chan struct{})
	go func() {
		c.writer()
		close(written)
	}()
	defer func() {
		c.closeAfterFlush()
		select {
		case <-written:
		case <-time.After(flushTimeout):
		}
	}()
	defer s.leave(c) // runs first: others see the user go at once
	conn.SetDeadline(time.Time{})
	for {
		conn.SetReadDeadline(time.Now().Add(idleTimeout))
		typ, body, err := readFrame(conn, maxFrame)
		if err != nil {
			return
		}
		s.message(c, typ, body)
	}
}

// join adds the client if its name is free. The whole initial state goes
// into its queue under the lock, so no later change can overtake it.
// join returns a Reject frame if the client cannot join.
func (s *Server) join(c *client, id identity, setup *pb.CryptSetup) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Again: a ban may have come (and kicked the account's sessions)
	// since identify. Bans are published under the lock (see removeUser).
	for _, hash := range []string{id.hash, certHash(c.certs)} {
		if reject := s.bannedReject(c.host, hash); reject != nil {
			return reject
		}
	}
	online := make([]Presence, 0, len(s.state.Users))
	voiceUsers, siblings := 0, 0
	for _, u := range s.state.Users {
		if !u.textOnly() {
			voiceUsers++
		}
		// The account's sessions stay, but a Mumble client replaces
		// another (see textonly.go).
		if id.registered && u.Registered && u.UserID == id.userID && (c.web || s.clients[u.Session].web) {
			siblings++
			continue
		}
		online = append(online, Presence{
			Session: u.Session, Name: u.Name, Registered: u.Registered, UserID: u.UserID,
			Host: s.clients[u.Session].host, Hash: u.Hash,
		})
	}
	ghost, inUse := findGhost(online, Presence{Name: id.name, Registered: id.registered, UserID: id.userID, Host: c.host, Hash: id.hash})
	if inUse {
		return mumble.RejectFrame(pb.Reject_UsernameInUse, "This name is in use.")
	}
	if siblings >= maxSessionsPerUser {
		return mumble.RejectFrame(pb.Reject_None, fmt.Sprintf("You are connected %d times already. Please close a tab or device.", siblings))
	}
	if voiceUsers >= s.maxUsers() && ghost == 0 && siblings == 0 && !(id.registered && id.userID == 0) {
		return mumble.RejectFrame(pb.Reject_ServerFull, fmt.Sprintf("The server is full (at most %d users).", s.maxUsers()))
	}
	stay := false // the same user on another device stays where it was
	if g := s.state.Users[ghost]; g != nil {
		if g.Registered && id.registered && g.UserID == id.userID {
			id.lastChannel, stay = g.Channel, true
		}
		s.kickLocked(ghost, 0, "You connected to the server from another device.", false)
	}
	s.next++
	c.session = s.next
	c.sendMsg(mumble.TypeVersion, &pb.Version{
		VersionV1: proto.Uint32(versionV1), VersionV2: proto.Uint64(versionV2), Release: proto.String("Kallisti"),
	})
	c.sendMsg(mumble.TypeCryptSetup, setup)
	c.sendMsg(mumble.TypeCodecVersion, &pb.CodecVersion{
		Alpha: proto.Int32(celt070), Beta: proto.Int32(0), PreferAlpha: proto.Bool(true), Opus: proto.Bool(true),
	})
	u := &User{Session: c.session, Name: c.name, Registered: id.registered, UserID: id.userID, GuestAccount: id.guestAccount,
		Since: id.since, Hash: id.hash, Tokens: id.tokens, Comment: id.comment, Texture: id.texture}
	// The last channel if the user may enter it, else root (as Murmur). Not
	// where root is a lobby (no voice in root): there users join voice on
	// purpose.
	// A web session joins text-only next to the account's voice session; a
	// Mumble client takes voice and its channel.
	voice := s.voiceSiblingLocked(u)
	if voice != nil && !c.web {
		id.lastChannel, stay = voice.Channel, true
	}
	lobby := !s.state.Has(u, 0, Speak)
	if ch := s.state.Channels[id.lastChannel]; ch != nil && (!lobby || stay) && s.state.Has(u, ch.ID, Enter) && s.state.Sees(u, ch.ID) {
		u.Channel = ch.ID
	}
	if !s.state.Has(u, u.Channel, Enter) {
		return mumble.RejectFrame(pb.Reject_None, "You may not enter this server.")
	}
	_, u.Mute = s.timedOutLocked(u)
	tookVoice := func() {}
	if voice != nil && c.web {
		u.Channel = TextOnly
	} else if voice != nil {
		tookVoice = s.takeVoiceLocked(u)
	}
	s.state.Refresh(u)
	c.seen = s.state.Visible(u)
	c.send(s.state.snapshot(u, c.seen), false)
	if !c.web {
		for _, su := range s.streams {
			if c.seen[su.channel] {
				c.send(su.state(), false)
			}
		}
	}
	s.state.AddUser(u)
	s.clients[c.session] = c
	if s.byHost[c.host] == nil {
		s.byHost[c.host] = map[*client]bool{}
	}
	s.byHost[c.host][c] = true
	tookVoice() // with c told if the voice session's channel goes
	s.sendUserLocked(u, userStateMsg(u))
	c.sendMsg(mumble.TypeServerSync, &pb.ServerSync{
		Session: proto.Uint32(c.session), MaxBandwidth: proto.Uint32(maxBandwidth),
		WelcomeText: proto.String(s.cfg.Welcome), Permissions: proto.Uint64(s.state.rootPerms(u)),
	})
	c.sendMsg(mumble.TypeServerConfig, &pb.ServerConfig{
		AllowHtml: proto.Bool(false), MessageLength: proto.Uint32(maxTextLength),
		ImageMessageLength: proto.Uint32(maxTextLength), MaxUsers: proto.Uint32(uint32(s.maxUsers())),
	})
	s.tellRecordingLocked(c.session, u.Channel)
	return nil
}

// snapshot is the channels (those seen) and users (in them) a joining
// user gets, as one queue entry: as single frames a big server would fill
// the queue.
func (s *State) snapshot(joining *User, seen map[uint32]bool) []byte {
	var out []byte
	var tree []*Channel
	for _, ch := range s.Tree() {
		if seen[ch.ID] {
			tree = append(tree, ch)
		}
	}
	for _, ch := range tree {
		out = append(out, mumble.MessageFrame(mumble.TypeChannelState, s.channelStateFor(ch, joining))...)
	}
	// Links once all channels exist, as Murmur does.
	for _, ch := range tree {
		if links := seenLinks(ch, seen); len(links) > 0 {
			out = append(out, mumble.MessageFrame(mumble.TypeChannelState,
				&pb.ChannelState{ChannelId: proto.Uint32(ch.ID), Links: links})...)
		}
	}
	for _, u := range s.Users {
		if seen[u.Channel] {
			out = append(out, mumble.MessageFrame(mumble.TypeUserState, forViewer(userStateMsg(u), seen))...)
		}
	}
	return out
}

func (s *Server) leave(c *client) {
	s.mu.Lock()
	if s.clients[c.session] != c {
		s.mu.Unlock()
		return // already replaced or kicked
	}
	u := s.removeLocked(c, &pb.UserRemove{Session: proto.Uint32(c.session)})
	s.promoteLocked(&u)
	s.mu.Unlock()
	s.saveLastChannel(u)
}

func (s *Server) saveLastChannel(u User) {
	if !u.Registered || u.textOnly() || s.cfg.DB == nil {
		return
	}
	if err := s.cfg.DB.SetLastChannel(u.UserID, u.Channel); err != nil {
		slog.Error("saving the last channel", "user", u.UserID, "err", err)
	}
}

// removeLocked takes a client off the server, tells everyone, and returns
// the user as it was.
func (s *Server) removeLocked(c *client, msg *pb.UserRemove) User {
	u := *s.state.Users[c.session]
	s.endStreamSoundLocked(c.session)
	delete(s.clients, c.session)
	if delete(s.byHost[c.host], c); len(s.byHost[c.host]) == 0 {
		delete(s.byHost, c.host)
	}
	if s.byAddr[c.udpAddr] == c {
		delete(s.byAddr, c.udpAddr)
	}
	removed := s.state.RemoveUser(c.session)
	if keys := s.timeoutKeysLocked(timeoutKeyOf(&u)); !u.Registered && len(keys) > 0 {
		for _, k := range keys {
			s.dropTimeoutLocked(k)
		}
		go s.timeoutsChanged()
	}
	frame := mumble.MessageFrame(mumble.TypeUserRemove, msg)
	for _, o := range s.clients {
		if o.seen[u.Channel] {
			o.send(frame, false)
		}
	}
	s.broadcastRemovedLocked(removed)
	return u
}

// kickLocked disconnects a user: it gets the reason, everyone else the
// removal. actor 0: the server.
func (s *Server) kickLocked(session, actor uint32, reason string, ban bool) {
	c := s.clients[session]
	if c == nil {
		return
	}
	msg := &pb.UserRemove{Session: proto.Uint32(session), Reason: proto.String(reason)}
	if ban {
		msg.Ban = proto.Bool(true)
	}
	if actor != 0 {
		msg.Actor = proto.Uint32(actor)
	}
	c.sendMsg(mumble.TypeUserRemove, msg)
	c.closeAfterFlush()
	c.conn.SetReadDeadline(time.Now()) // nothing more from it
	u := s.removeLocked(c, msg)
	go s.saveLastChannel(u)
}

// EndPasswordLogins disconnects the Mumble clients of the web account
// with this certificate: they logged in with its password (see
// identifyAccount), which changed or was logged out everywhere. Its web
// sessions end with their own logins.
func (s *Server) EndPasswordLogins(certHash string) {
	if certHash == "" { // none: clients without a certificate are not its
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for session, c := range s.clients {
		if u := s.state.Users[session]; !c.web && u != nil && u.Hash == certHash {
			s.kickLocked(session, 0, "You were logged out.", false)
		}
	}
}

// channelState is the full ChannelState of a channel, without links.
func channelState(ch *Channel) *pb.ChannelState {
	m := &pb.ChannelState{
		ChannelId: proto.Uint32(ch.ID), Name: proto.String(ch.Name),
		Position: proto.Int32(ch.Position), Temporary: proto.Bool(ch.Temporary),
	}
	m.Description, m.DescriptionHash = commentFields(ch.Description)
	if ch.ID != 0 {
		m.Parent = proto.Uint32(ch.Parent)
	}
	return m
}

// userStateMsg is the full UserState of a user.
func userStateMsg(u *User) *pb.UserState {
	m := &pb.UserState{Session: proto.Uint32(u.Session), Name: proto.String(u.Name), ChannelId: proto.Uint32(u.Channel)}
	// Only flags that are set: clients read every field as a change.
	flag := func(on bool) *bool {
		if on {
			return proto.Bool(true)
		}
		return nil
	}
	m.Mute, m.Deaf, m.Suppress = flag(u.Mute), flag(u.Deaf), flag(u.Suppress)
	m.SelfMute, m.SelfDeaf = flag(u.SelfMute), flag(u.SelfDeaf)
	m.PrioritySpeaker, m.Recording = flag(u.PrioritySpeaker), flag(u.Recording)
	if u.Registered {
		m.UserId = proto.Uint32(u.UserID)
	}
	if u.Hash != "" {
		m.Hash = proto.String(u.Hash)
	}
	m.ListeningChannelAdd = slices.Sorted(maps.Keys(u.Listening))
	setUserBlobs(m, u)
	return m
}

// broadcastRemovedLocked tells those who saw them about removed channels.
func (s *Server) broadcastRemovedLocked(removed []uint32) {
	s.stopRecordingsLocked(removed, "channel deleted")
	for _, id := range removed {
		frame := mumble.MessageFrame(mumble.TypeChannelRemove, &pb.ChannelRemove{ChannelId: proto.Uint32(id)})
		for _, c := range s.clients {
			if c.seen[id] {
				delete(c.seen, id)
				c.send(frame, false)
			}
		}
	}
}
