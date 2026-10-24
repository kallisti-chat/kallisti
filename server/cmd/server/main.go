// Command server is the Kallisti server: the web app, accounts, text
// channels, and voice through its built-in Mumble server (or a Murmur at
// MUMBLE_HOST).
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"
	"time"

	"kallisti.chat/server/internal/auth"
	"kallisti.chat/server/internal/backup"
	"kallisti.chat/server/internal/bridge"
	"kallisti.chat/server/internal/certs"
	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/emoji"
	"kallisti.chat/server/internal/fedi"
	"kallisti.chat/server/internal/files"
	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/preview"
	"kallisti.chat/server/internal/push"
	"kallisti.chat/server/internal/recording"
	"kallisti.chat/server/internal/sidecar"
	"kallisti.chat/server/internal/site"
	"kallisti.chat/server/internal/store"
	"kallisti.chat/server/internal/stream"
	"kallisti.chat/server/internal/thumbs"
	"kallisti.chat/server/internal/whip"
)

// A streamer's upload, at most: 8 Mbit/s (frames above it are dropped);
// streams from OBS at the same time, at most.
const (
	streamBytesPerSecond = 1 << 20
	whipMaxSessions      = 20
)

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Getenv)
	if err != nil {
		return err
	}
	if slices.Contains(cfg.AllowedOrigins, "*") {
		slog.Warn("ALLOWED_ORIGINS is *: every origin can connect (development only)")
	}

	// A copy before the database is opened: updates migrate it (one-way).
	if cfg.BackupDir != "" {
		if path, err := backup.BeforeStart(cfg.DBPath, cfg.BackupDir, cfg.BackupKeep, time.Now()); err != nil {
			return fmt.Errorf("backup before the start: %w", err)
		} else if path != "" {
			slog.Info("database backup before the start", "file", path)
		}
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database %s: %w", cfg.DBPath, err)
	}
	defer db.Close()
	if err := db.RenameReactionEmoji(emoji.Normal); err != nil {
		return fmt.Errorf("reactions in the current emoji form: %w", err)
	}
	// With the built-in server, admins manage the text channels: the
	// configured ones only seed a new server.
	setUp := db.EnsureChannels
	if cfg.Builtin != nil {
		setUp = db.SeedChannels
	}
	if err := setUp(cfg.TextChannels); err != nil {
		return fmt.Errorf("set up text channels: %w", err)
	}

	hub, err := sidecar.NewHub(db, time.Now)
	if err != nil {
		return fmt.Errorf("load text channels: %w", err)
	}
	accounts := &auth.Service{
		Store:             db,
		Now:               time.Now,
		AllowedOrigins:    cfg.AllowedOrigins,
		TrustForwardedFor: cfg.TrustForwardedFor,
		ServerPassword:    cfg.Builtin == nil || cfg.Builtin.Password != "",
		NoRegistration:    cfg.NoRegistration,
	}
	accounts.Fedi = fediLogin(cfg)
	// Before the built-in server listens: Mumble messages may bring images.
	if err := os.MkdirAll(cfg.UploadDir, 0o700); err != nil {
		return fmt.Errorf("create upload directory: %w", err)
	}
	fileHandler := &files.Handler{
		Store:           db,
		Dir:             cfg.UploadDir,
		MaxBytes:        cfg.UploadMaxBytes,
		QuotaBytes:      cfg.UploadQuotaBytes,
		AllowedOrigins:  cfg.AllowedOrigins,
		Now:             time.Now,
		Redeem:          hub.RedeemUploadToken,
		RedeemEmoji:     hub.RedeemEmojiToken,
		OnEmoji:         hub.EmojiChanged,
		RedeemUserEmoji: hub.RedeemUserEmojiToken,
		OnUserEmoji:     hub.UserEmojiChanged,
		RedeemSticker:   hub.RedeemStickerToken,
		OnSticker:       hub.StickersChanged,
		OnPreview:       hub.UploadPreview,
	}
	// Once: uploads from before sizes were kept.
	if err := db.MeasureUploads(fileHandler.Measure); err != nil {
		return fmt.Errorf("measure uploads: %w", err)
	}
	// ffmpeg's jobs end with the server.
	previewCtx, stopPreviews := context.WithCancel(context.Background())
	defer stopPreviews()
	startPreviews(previewCtx, cfg, fileHandler)
	var linkImages http.Handler = http.NotFoundHandler()
	if !cfg.NoLinkPreviews {
		previews := preview.New()
		hub.Previews, linkImages = previews, previews
	}
	hub.OnUploadDeleted = fileHandler.Remove
	hub.UploadMax = cfg.UploadMaxBytes
	hub.SaveFile = fileHandler.Save
	// Before the built-in server listens, too.
	var pushes http.Handler = http.NotFoundHandler() // not app routes
	if cfg.Builtin != nil && !cfg.Push.Off {
		svc, err := startPush(cfg, db, accounts, hub)
		if err != nil {
			return err
		}
		defer svc.Stop()
		pushes = svc
	}
	dial, upstream := murmurDialer(cfg)
	var builtinServer *builtin
	cleanup := func(time.Time) {}                // guest accounts (built-in server)
	var avatars http.Handler = mumbled.NoAvatars // Murmur's avatars are not ours to read
	if cfg.Builtin != nil {
		mumble, err := startBuiltin(cfg.Builtin, db, hub, publicBase(cfg.AllowedOrigins))
		if err != nil {
			return err
		}
		builtinServer = mumble
		defer mumble.close()
		dial, upstream = mumble.dial, "built-in on "+cfg.Builtin.Listen
		accounts.NameTaken = mumble.nameTaken
		accounts.MumbleLogin = mumble.srv.CheckPassword
		accounts.GuestAccounts = true // the built-in server can remove their registrations
		accounts.Captcha = mumble.srv.Captcha
		accounts.EndPasswordLogins = func(acc store.Account) { mumble.srv.EndPasswordLogins(accountCertHash(acc)) }
		accounts.SetAvatar = func(acc store.Account, avatar []byte, onlyIfNone bool) {
			if _, err := mumble.srv.SetAccountAvatar(acc.Name, accountCertHash(acc), avatar, onlyIfNone); err != nil {
				slog.Error("set an account's avatar", "account", acc.Name, "err", err)
			}
		}
		accounts.OnPermanent = func(name string) {
			mumble.srv.AccountPermanent(name) // its users may write again
			hub.AccountPermanent(name)
		}
		cleanup = func(now time.Time) {
			cleanGuests(db, now, mumble.srv.Unregister)
			mumble.recordings.Clean()
		}
		if cfg.Builtin.Password == "" { // a private server keeps them private
			avatars, hub.Avatars = mumble.srv.Avatars(), true
		}
	}
	b := bridge.New(bridge.Options{
		Sidecar: hub,
		// Murmur sees all web users at our IP and kicks the older session of
		// a name from the same IP; the built-in server gets the real IPs.
		UniqueGuestNames:  cfg.Builtin == nil,
		AllowedOrigins:    cfg.AllowedOrigins,
		MaxConnections:    cfg.MaxConnections,
		MaxPerIP:          cfg.MaxPerIP,
		TrustForwardedFor: cfg.TrustForwardedFor,
		Dial:              dial,
		Account: func(r *http.Request) (bridge.Account, bool) {
			acc, ok := accounts.AccountFor(r)
			if !ok {
				return bridge.Account{}, false
			}
			db.TouchAccount(acc.ID, time.Now()) // a visit (guest accounts live a day from it)
			cert, err := certs.Load(acc.CertPEM, acc.KeyPEM)
			if err != nil {
				slog.Error("load account certificate", "account", acc.Name, "err", err)
				return bridge.Account{}, false
			}
			return bridge.Account{Name: acc.Name, Cert: &cert, ID: acc.ID, Session: auth.SessionKey(r)}, true
		},
		IsAccountName: accountExists(db),
	})
	// Ended sessions end their open connections.
	accounts.Revoke = b.Revoke

	if cfg.BackupDir != "" {
		go backup.Daily(db.DB(), cfg.BackupDir, cfg.BackupKeep)
	}
	go func() {
		for range time.Tick(time.Minute) {
			fileHandler.DeleteUnused()
			hub.ArchiveIdleThreads()
			hub.EndDuePolls()
			cleanup(time.Now())
			if err := accounts.DeleteExpiredSessions(); err != nil {
				slog.Error("delete expired sessions", "err", err)
			}
		}
	}()
	mux := http.NewServeMux()
	mux.Handle("/api/upload", fileHandler)
	for _, path := range []string{"/api/server", "/api/challenge", "/api/accounts", "/api/guest", "/api/login", "/api/logout", "/api/logout-all", "/api/password", "/api/me", "/api/stream-key", "/api/fedi", "/api/fedi/"} {
		mux.Handle(path, accounts)
	}
	mux.Handle("/files/", fileHandler)
	mux.Handle("/api/emoji", fileHandler)
	mux.Handle("/api/emoji/", fileHandler)
	mux.Handle("/api/stickers", fileHandler)
	mux.Handle("/api/stickers/", fileHandler)
	mux.Handle("/api/avatars/", avatars)
	mux.Handle("/api/link-image/", linkImages)
	mux.Handle("/api/push", pushes)
	mux.Handle("/api/push/", pushes)
	if builtinServer != nil {
		mux.Handle("/api/stream", &stream.Handler{
			Hub: builtinServer.streams, Redeem: builtinServer.streamTokens.Redeem,
			AllowedOrigins: cfg.AllowedOrigins, BytesPerSecond: streamBytesPerSecond,
		})
		if cfg.Builtin.WHIPPort != 0 {
			// Chat and voice go on without it (a name that did not resolve).
			if wh, err := startWHIP(cfg.Builtin, builtinServer, accounts, fileHandler.Previews); err != nil {
				slog.Error("streams from OBS are off", "err", err)
			} else {
				defer wh.Close()
				mux.Handle(whip.Path, wh)
				mux.Handle(whip.Path+"/", wh)
			}
		}
		mux.Handle("/api/recordings/", &recording.Handler{
			Library: builtinServer.recordings, Redeem: builtinServer.recordingTokens.Redeem, MayDownload: builtinServer.srv.MayRecord,
		})
	}
	// WebSocket upgrades (the client uses /ws) go to the bridge, everything
	// else to the web app.
	mux.Handle("/", site.New(b, cfg.WebDir))

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.ListenPort))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", cfg.ListenPort, err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	slog.Info("server running", "listen", ln.Addr().String(), "mumble", upstream,
		"db", cfg.DBPath, "text_channels", cfg.TextChannels)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	// First stop accepting. Shutdown does not wait for hijacked (WebSocket)
	// connections: the bridge closes and waits for those itself.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	b.Close()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Push workers, and pushes that may wait for one (more are dropped).
const (
	pushWorkers = 16
	pushQueue   = 1024
)

// startPush turns on Web Push notifications: the hub decides who gets one,
// the service sends them.
func startPush(cfg config.Config, db *store.Store, accounts *auth.Service, hub *sidecar.Hub) (*push.Service, error) {
	public, private, err := push.Keys(db, cfg.Push.PublicKey, cfg.Push.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("push keys: %w", err)
	}
	svc := &push.Service{
		Store: db, PublicKey: public, Now: time.Now,
		Sender: push.NewWebPush(public, private, cfg.Push.Subject, publicBase(cfg.AllowedOrigins)),
		Account: func(r *http.Request) (store.Account, string, bool) {
			acc, ok := accounts.AccountFor(r)
			return acc, auth.SessionKey(r), ok
		},
	}
	svc.Start(pushWorkers, pushQueue)
	hub.Push = svc.Notify
	return svc, nil
}

// Stream previews from OBS: as browsers make theirs (stream.previewSize).
const (
	streamPreviewWidth  = 320
	streamPreviewHeight = 1024
)

// Previews: ffmpeg processes at once, and new uploads waiting for one.
const (
	previewWorkers = 2
	previewQueue   = 64
)

// startPreviews makes upload previews if ffmpeg is there (and they are not
// turned off), and those older uploads lack in the background.
func startPreviews(ctx context.Context, cfg config.Config, h *files.Handler) {
	if cfg.NoMediaPreviews {
		slog.Info("media previews off (MEDIA_PREVIEWS=0)")
		return
	}
	tool, err := thumbs.Find(cfg.FFmpegPath)
	if err != nil {
		log := slog.Info
		if cfg.FFmpegPath != "" { // asked for
			log = slog.Warn
		}
		log("media previews off: no usable ffmpeg", "err", err)
		return
	}
	slog.Info("media previews on", "ffmpeg", tool.Command[len(tool.Command)-1], "format", tool.Format.Type)
	h.Previews = thumbs.New(ctx, tool, previewWorkers, previewQueue)
	go h.MakeMissingPreviews(ctx)
}

// murmurDialer connects web clients to Murmur. Accounts connect with their
// own client certificate, so Murmur can register them; guests without one.
func murmurDialer(cfg config.Config) (func(context.Context, bridge.Upstream) (net.Conn, error), string) {
	upstream := net.JoinHostPort(cfg.MumbleHost, strconv.Itoa(cfg.MumblePort))
	tlsConfig := &tls.Config{InsecureSkipVerify: !cfg.RejectUnauthorized}
	// SNI does not allow IP addresses.
	if net.ParseIP(cfg.MumbleHost) == nil {
		tlsConfig.ServerName = cfg.MumbleHost
	}
	return func(ctx context.Context, up bridge.Upstream) (net.Conn, error) {
		conf := tlsConfig.Clone()
		if up.Cert != nil {
			conf.Certificates = []tls.Certificate{*up.Cert}
		}
		d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: conf}
		return d.DialContext(ctx, "tcp", upstream)
	}, upstream
}

// publicBase is the server's public address (for upload links that Mumble
// clients get): the first allowed origin, unless that is "*".
func publicBase(origins []string) string {
	for _, o := range origins {
		if o != "*" {
			return o
		}
	}
	return ""
}

// fediLogin: logins with fediverse accounts, unless turned off or without
// a public address (instances send the browser back to it).
func fediLogin(cfg config.Config) *auth.FediLogin {
	public := publicBase(cfg.AllowedOrigins)
	if cfg.NoFediLogin || public == "" {
		return nil
	}
	name := "Kallisti"
	if cfg.Builtin != nil {
		name = cfg.Builtin.ServerName
	}
	var allow func(netip.AddrPort) bool // public addresses, HTTPS
	if cfg.FediInsecureDev {
		slog.Warn("FEDI_INSECURE_DEV_INSTANCES is 1: fediverse logins may reach local addresses (development only)")
		allow = func(netip.AddrPort) bool { return true }
	}
	return &auth.FediLogin{Client: fedi.NewClient(allow, nil, cfg.FediInsecureDev), Public: public, AppName: name, Dev: cfg.FediInsecureDev}
}

// startWHIP takes streams from OBS on the UDP port (WHIP_UDP_PORT), with
// the accounts' stream keys.
func startWHIP(cfg *config.Builtin, b *builtin, accounts *auth.Service, previews *thumbs.Maker) (*whip.Handler, error) {
	settings, udp, err := whip.Listen(cfg.WHIPPort, cfg.WHIPPublicIP)
	if err != nil {
		return nil, err
	}
	h := &whip.Handler{
		Settings: settings, Hub: b.streams, BytesPerSecond: streamBytesPerSecond, MaxSessions: whipMaxSessions,
		Account: func(key string) (string, bool) {
			acc, ok := accounts.StreamKeyAccount(key)
			return acc.Name, ok
		},
		Session: b.sessionOf,
		Closer:  udp,
	}
	if previews != nil { // with ffmpeg, as upload previews
		h.Preview = func(ctx context.Context, keyframe []byte) ([]byte, error) {
			return previews.Still(ctx, keyframe, streamPreviewWidth, streamPreviewHeight)
		}
	}
	accounts.StreamKeys, accounts.StreamKeyChanged = true, h.EndAccount
	slog.Info("streams from OBS (WHIP)", "udp_port", cfg.WHIPPort, "public_ip", cfg.WHIPPublicIP)
	return h, nil
}
