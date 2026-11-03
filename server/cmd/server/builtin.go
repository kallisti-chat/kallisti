package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"time"

	"kallisti.chat/server/internal/bridge"
	"kallisti.chat/server/internal/certs"
	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/password"
	"kallisti.chat/server/internal/recording"
	"kallisti.chat/server/internal/sidecar"
	"kallisti.chat/server/internal/store"
	"kallisti.chat/server/internal/stream"
)

// builtin is the built-in Mumble server: native clients (Mumble, the
// desktop app) connect to its port, web clients in-process through the
// bridge.
type builtin struct {
	srv *mumbled.Server
	db  *mumbled.DB
	ln  net.Listener
	udp *net.UDPConn
	// Screen shares in its voice channels (see /api/stream).
	streams      *stream.Hub
	streamTokens *stream.Tokens
	// Channel recordings, and tokens for their downloads (see
	// /api/recordings/).
	recordings      *recording.Library
	recordingTokens *stream.Tokens
	stopWatch       chan struct{}
}

// recorder is the built-in server's recordings as the sidecar's Recorder.
type recorder struct {
	*mumbled.Server
	lib    *recording.Library
	tokens *stream.Tokens
}

func (r recorder) Finished() []recording.Info          { return r.lib.List() }
func (r recorder) DownloadToken(session uint32) string { return r.tokens.Issue(session) }

// streamVoice is the built-in server as the stream hub's Voice (set once
// the server exists: the hub follows moves from the start).
type streamVoice struct{ srv *mumbled.Server }

func (v *streamVoice) Channel(session uint32) (uint32, bool) { return v.srv.UserChannel(session) }
func (v *streamVoice) MaySpeak(session, channel uint32) bool { return v.srv.MaySpeak(session, channel) }

// accountExists: on a database error the name counts as taken.
func accountExists(db *store.Store) func(name string) bool {
	return func(name string) bool {
		_, err := db.AccountByName(name)
		return !errors.Is(err, store.ErrNotFound)
	}
}

// accountLogin: Mumble clients may log in with an account's password.
// Guest accounts have none; on a database error the name stays reserved.
// Names without an account skip argon2: ReservedName tells them anyway.
func accountLogin(db *store.Store) func(name, pw string) (*mumbled.WebAccount, bool) {
	return func(name, pw string) (*mumbled.WebAccount, bool) {
		acc, err := db.AccountByName(name)
		if err != nil || acc.Temporary || acc.PasswordHash == "" {
			return nil, true
		}
		release := password.Limit()
		ok := password.Check(acc.PasswordHash, pw)
		release()
		if !ok {
			return nil, false
		}
		return &mumbled.WebAccount{Name: acc.Name, CertHash: accountCertHash(acc)}, true
	}
}

// startBuiltin starts the server. With a hub, voice channels get stored
// chats (publicBase: the server's address, for upload links).
func startBuiltin(cfg *config.Builtin, db *store.Store, hub *sidecar.Hub, publicBase string) (*builtin, error) {
	mdb, err := mumbled.OpenDB(db.DB())
	if err != nil {
		return nil, fmt.Errorf("Mumble tables: %w", err)
	}
	if err := mdb.InitLobby(); err != nil {
		return nil, fmt.Errorf("lobby: %w", err)
	}
	if err := mdb.InitRoles(); err != nil {
		return nil, fmt.Errorf("roles: %w", err)
	}
	// SuperUser's password, from the environment (not visible in ps).
	if pw := os.Getenv("MUMBLED_SUPERUSER_PASSWORD"); pw != "" {
		hash, err := password.Hash(pw)
		if err != nil {
			return nil, err
		}
		if err := mdb.SetPassword(0, hash); err != nil {
			return nil, err
		}
		os.Unsetenv("MUMBLED_SUPERUSER_PASSWORD")
	}
	tlsConfig, err := mumbleTLS(cfg)
	if err != nil {
		return nil, err
	}
	var onText func(mumbled.ChannelText)
	var checkText func(session, channel uint32) error
	var onPrivate func(mumbled.PrivateText)
	var onRoles, onListenOnly, onPrivateChannels, onTimeouts, onGuestsReadOnly, onRecording func()
	var onUser func(id uint32, name string)
	var onPicture func(id uint32, picture bool)
	var textOverflows func() map[uint32]uint64
	sv := &streamVoice{}
	recordings := &recording.Library{
		Dir: cfg.RecordingDir, Keep: cfg.RecordingKeep, Now: time.Now,
		Limits: recording.Limits{MaxDuration: cfg.RecordingMax, MaxBytes: cfg.RecordingMaxBytes},
	}
	streams, streamTokens := stream.New(sv), stream.NewTokens()
	onMoved := func(session, _, to uint32) { streams.Moved(session, to) }
	if hub != nil {
		onRoles, onListenOnly, onPrivateChannels, onTimeouts = hub.RolesChanged, hub.ListenOnlyChanged, hub.PrivateChanged, hub.TimeoutsChanged
		onGuestsReadOnly, onRecording = hub.GuestsReadOnlyChanged, hub.RecordingChanged
		checkText = hub.MumbleTextAllowed
		onUser, onPicture = hub.UserChanged, hub.PictureChanged
		textOverflows = hub.Overflows
		onMoved = func(session, from, to uint32) {
			hub.UserMoved(session, from, to)
			streams.Moved(session, to)
		}
		liveStreams := func(infos []stream.Info) []sidecar.LiveStream {
			out := make([]sidecar.LiveStream, len(infos))
			for i, s := range infos {
				out[i] = sidecar.LiveStream{Session: s.Session, Channel: s.Channel, Preview: s.Preview}
			}
			return out
		}
		// Streams with sound have a stream user for Mumble clients.
		streams.OnChange = func(infos []stream.Info) {
			hub.StreamsChanged(liveStreams(infos))
			var withSound []uint32
			for _, i := range infos {
				if i.Audio {
					withSound = append(withSound, i.Session)
				}
			}
			sv.srv.SetStreamSounds(withSound)
		}
		streams.OnSound = func(streamer uint32, opus []byte, ts uint64) { sv.srv.StreamSound(streamer, opus, ts) }
		streams.OnPreview = func(i stream.Info) { hub.StreamPreview(liveStreams([]stream.Info{i})[0]) }
		hub.LiveStreams = func() []sidecar.LiveStream { return liveStreams(streams.Streams()) }
		hub.StreamToken = streamTokens.Issue
		onText = func(t mumbled.ChannelText) {
			from := mumble.Identity{Session: t.From, Name: t.Name, Registered: t.Registered, UserID: t.UserID}
			hub.MumbleText(from, t.Channel, t.Text, sidecarImages(t.Images))
		}
		onPrivate = func(p mumbled.PrivateText) {
			from := mumble.Identity{Session: p.From, Name: p.Name, Registered: true, UserID: p.UserID}
			hub.MumblePrivateText(from, p.To, p.Text, sidecarImages(p.Images), p.Delivered)
		}
	}
	srv, err := mumbled.New(mumbled.Config{
		Welcome: cfg.Welcome, RootName: cfg.ServerName, Password: cfg.Password, DB: mdb,
		MaxUsers: cfg.MaxUsers, MaxPerIP: cfg.MaxPerIP,
		ReservedName: accountExists(db), // names of web accounts are theirs only
		AllowGuest: func() bool {
			on, err := db.InviteOnly()
			return err == nil && !on
		},
		WebAccountExists: func(name, hash string) bool {
			acc, err := db.AccountByName(name)
			return err == nil && accountCertHash(acc) == hash
		},
		GuestAccount: func(name string) bool {
			acc, err := db.AccountByName(name)
			return err == nil && acc.Temporary
		},
		AccountLogin:            accountLogin(db),
		OnChannelText:           onText,
		CheckChannelText:        checkText,
		OnPrivateText:           onPrivate,
		OnRolesChanged:          onRoles,
		OnUserChanged:           onUser,
		OnPictureChanged:        onPicture,
		OnListenOnlyChanged:     onListenOnly,
		OnPrivateChanged:        onPrivateChannels,
		OnTimeoutsChanged:       onTimeouts,
		OnGuestsReadOnlyChanged: onGuestsReadOnly,
		OnUserMoved:             onMoved,
		TextOverflows:           textOverflows,
		Recordings:              recordings,
		OnRecordingChanged:      onRecording,
	})
	if err != nil {
		return nil, err
	}
	sv.srv = srv
	recordingTokens := stream.NewTokens()
	if hub != nil {
		hub.SetVoice(voiceServer{srv, streams}, publicBase) // before anyone connects
		hub.Recorder = recorder{srv, recordings, recordingTokens}
	}
	ln, err := tls.Listen("tcp", cfg.Listen, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("Mumble port: %w", err)
	}
	udpAddr, err := net.ResolveUDPAddr("udp", cfg.Listen)
	if err != nil {
		ln.Close()
		return nil, err
	}
	udp, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("Mumble UDP port: %w", err)
	}
	go srv.Serve(ln)
	go srv.ServeUDP(udp)
	stopWatch := make(chan struct{})
	go srv.Watch(stopWatch)
	return &builtin{srv: srv, db: mdb, ln: ln, udp: udp, streams: streams, streamTokens: streamTokens,
		recordings: recordings, recordingTokens: recordingTokens, stopWatch: stopWatch}, nil
}

// mumbleTLS: the configured certificate files (reloaded when renewed), or
// a self-signed certificate made on the first start (CERT_DIR).
func mumbleTLS(cfg *config.Builtin) (*tls.Config, error) {
	cert, err := certs.LoadOrCreateServer(cfg.CertDir)
	if err != nil {
		return nil, fmt.Errorf("server certificate in %s: %w", cfg.CertDir, err)
	}
	if cfg.CertFile == "" {
		return mumbled.TLSConfig(cert), nil
	}
	// Until the files exist (a new proxy), the self-signed one.
	files, err := certs.NewFileSource(cfg.CertFile, cfg.KeyFile, &cert)
	if err != nil {
		return nil, fmt.Errorf("Mumble certificate: %w", err)
	}
	return mumbled.TLSConfigFrom(files.GetCertificate), nil
}

func sidecarImages(images []mumbled.Image) []sidecar.Image {
	out := make([]sidecar.Image, len(images))
	for i, img := range images {
		out[i] = sidecar.Image{Type: img.Type, Data: img.Data}
	}
	return out
}

// voiceServer is the built-in server as the sidecar's Voice.
// voiceServer: streams for what their viewers missed (see Connections).
type voiceServer struct {
	*mumbled.Server
	streams *stream.Hub
}

func (v voiceServer) SyncTextTree(cats []sidecar.MirrorCategory, want []sidecar.Mirror) ([]uint32, []uint32, error) {
	categories := make([]mumbled.TextCategory, len(cats))
	for i, c := range cats {
		categories[i] = mumbled.TextCategory{Channel: c.Channel, Name: c.Name}
	}
	mirrors := make([]mumbled.TextMirror, len(want))
	for i, w := range want {
		mirrors[i] = mumbled.TextMirror{Channel: w.Channel, Name: w.Name, Topic: w.Topic, Private: w.Private, Category: w.Category}
	}
	return v.Server.SyncTextTree(categories, mirrors)
}

func (v voiceServer) MumbleClient(session uint32) (sidecar.MumbleUser, bool) {
	u, ok := v.Server.MumbleClient(session)
	return sidecar.MumbleUser{Registered: u.Registered, UserID: u.UserID}, ok
}

func (v voiceServer) Online() map[uint32]sidecar.OnlineUser {
	out := map[uint32]sidecar.OnlineUser{}
	for id, u := range v.Server.Online() {
		out[id] = sidecar.OnlineUser{Name: u.Name, Session: u.Session}
	}
	return out
}

func (v voiceServer) ListenOnly() []sidecar.ListenOnlyChannel {
	var out []sidecar.ListenOnlyChannel
	for _, c := range v.Server.ListenOnly() {
		speakers := make([]sidecar.Speaker, len(c.Speakers))
		for i, sp := range c.Speakers {
			speakers[i] = sidecar.Speaker{ID: sp.ID, Name: sp.Name}
		}
		out = append(out, sidecar.ListenOnlyChannel{Channel: c.Channel, Speakers: speakers})
	}
	return out
}

func (v voiceServer) Private() []sidecar.PrivateChannel {
	conv := func(ms []mumbled.Member) []sidecar.Member {
		out := make([]sidecar.Member, len(ms))
		for i, m := range ms {
			out[i] = sidecar.Member{ID: m.ID, Name: m.Name}
		}
		return out
	}
	var out []sidecar.PrivateChannel
	for _, c := range v.Server.Private() {
		out = append(out, sidecar.PrivateChannel{Channel: c.Channel, Members: conv(c.Members), Inherited: conv(c.Inherited)})
	}
	return out
}

func (v voiceServer) Timeouts() []sidecar.Timeout { return sidecarTimeouts(v.Server.Timeouts()) }

func (v voiceServer) ChannelTimeouts() []sidecar.Timeout {
	return sidecarTimeouts(v.Server.ChannelTimeouts())
}

func sidecarTimeouts(list []mumbled.Timeout) []sidecar.Timeout {
	out := make([]sidecar.Timeout, len(list))
	for i, t := range list {
		out[i] = sidecar.Timeout(t)
	}
	return out
}

func (v voiceServer) ChannelModerators() []sidecar.ChannelModerators {
	var out []sidecar.ChannelModerators
	for _, m := range v.Server.ChannelModerators() {
		out = append(out, sidecar.ChannelModerators(m))
	}
	return out
}

func (v voiceServer) Connections(session uint32) ([]sidecar.Connection, error) {
	conns, err := v.Server.Connections(session)
	drops := v.streams.Drops()
	out := make([]sidecar.Connection, len(conns))
	for i, c := range conns {
		out[i] = sidecar.Connection(c)
		d := drops[c.Session]
		out[i].StreamPictures, out[i].StreamSound = d.Pictures, d.Sound
	}
	return out, err
}

// dial makes an in-process session for a web client.
func (b *builtin) dial(_ context.Context, up bridge.Upstream) (net.Conn, error) {
	client, server := net.Pipe()
	o := mumbled.Origin{}
	if ip, err := netip.ParseAddr(up.IP); err == nil {
		o.Host = ip.Unmap()
	}
	if up.Cert != nil && up.AccountName != "" {
		o.Account = &mumbled.WebAccount{Name: up.AccountName, CertHash: certHash(up.Cert.Certificate[0])}
	}
	go b.srv.ServeConn(server, o)
	return client, nil
}

// sessionOf is the voice session of a registered user, by name.
func (b *builtin) sessionOf(name string) (uint32, bool) {
	key := names.Key(name)
	for _, u := range b.srv.Online() {
		if names.Key(u.Name) == key {
			return u.Session, true
		}
	}
	return 0, false
}

// nameTaken: new web accounts may not take names of Mumble registrations.
func (b *builtin) nameTaken(key string) bool {
	r, err := b.db.UserByName(key)
	if err != nil {
		slog.Error("looking up a Mumble user", "err", err)
		return true
	}
	return r != nil
}

func (b *builtin) close() {
	close(b.stopWatch)
	b.ln.Close()
	b.udp.Close()
	b.srv.Close()
}
