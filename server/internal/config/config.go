// Package config reads the server settings from environment variables.
package config

import (
	"cmp"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// The Mumble server: the built-in one (Builtin), or Murmur at
	// MumbleHost when that is set.
	Builtin    *Builtin
	MumbleHost string
	MumblePort int
	ListenPort int
	// "*" allows every origin (development only).
	AllowedOrigins []string
	// Most Mumble servers use self-signed certificates.
	RejectUnauthorized bool
	MaxConnections     int
	// SQLite file for text channels and messages.
	DBPath string
	// Text channel names, in display order.
	TextChannels []string
	// Web connections from one IP. With Murmur, all web users reach it from
	// our IP, so one client must not use them all (its autoban would hit
	// everyone).
	MaxPerIP int
	// Use the last X-Forwarded-For address (the one our proxy added) as the
	// client IP (see clientip). Only behind one reverse proxy that sets it:
	// clients can fake the header.
	TrustForwardedFor bool
	// LINK_PREVIEWS=0: the server fetches no pages for link previews.
	NoLinkPreviews bool
	// FEDI_LOGIN=0: no logins with fediverse accounts. They also need a
	// public address (an ALLOWED_ORIGINS entry other than *).
	NoFediLogin bool
	// FEDI_INSECURE_DEV_INSTANCES=1, for development and tests only:
	// fediverse instances may be on local addresses and plain HTTP (no
	// protection against requests into the local network).
	FediInsecureDev bool
	// MEDIA_PREVIEWS=0: no video thumbnails and picture previews, even
	// with ffmpeg (FFMPEG_PATH, else on PATH).
	NoMediaPreviews bool
	FFmpegPath      string
	// Opt-in experiment: room media requires current room access (see
	// docs/configuration.md). NoSignedLinks: only by a session, never by
	// signed links (the apps and other servers' tabs show no private files).
	ExperimentalPrivateRoomMedia  bool
	PrivateRoomMediaNoSignedLinks bool
	// Translation models for the clients (see translation): the languages
	// paired with English ("*": all; none: off), kept in TranslationDir.
	TranslationLanguages []string
	TranslationDir       string
	// Web Push notifications (built-in server).
	Push Push
	// Uploaded files, and the size limit per file.
	UploadDir string
	// BackupDir: daily database backups go here (empty: none); the newest
	// BackupKeep stay.
	BackupDir      string
	BackupKeep     int
	UploadMaxBytes int64
	// Bytes one user may upload per 24 hours.
	UploadQuotaBytes int64
	// The built web app, served from the same origin. Empty: not served
	// (development, where Vite serves it).
	WebDir string
}

// list splits a comma-separated value, trims and drops empty and repeated
// entries.
func list(value string) []string {
	var out []string
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func positive(value string, fallback int) (int, bool) {
	if value == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(value)
	return n, err == nil && n > 0
}

func parsePort(value string, fallback int) (int, bool) {
	if value == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(value)
	return n, err == nil && n > 0 && n < 65536
}

// Push configures Web Push. PUSH=0: off. Without keys the server makes
// them on the first start and keeps them in the database. Subject: a
// contact for push services (empty: the server's public address).
type Push struct {
	Off                   bool
	PublicKey, PrivateKey string
	Subject               string
}

func parsePush(getenv func(string) string) (Push, error) {
	p := Push{Off: getenv("PUSH") == "0", PublicKey: getenv("VAPID_PUBLIC_KEY"), PrivateKey: getenv("VAPID_PRIVATE_KEY"), Subject: getenv("PUSH_SUBJECT")}
	if (p.PublicKey == "") != (p.PrivateKey == "") {
		return Push{}, errors.New("set both VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY, or neither")
	}
	if p.Subject != "" && !strings.HasPrefix(p.Subject, "mailto:") && !strings.HasPrefix(p.Subject, "https:") {
		return Push{}, errors.New("PUSH_SUBJECT must be a mailto: or https: URL")
	}
	return p, nil
}

// Builtin configures the built-in Mumble server.
type Builtin struct {
	Listen string // TCP and UDP for Mumble clients
	// PublicPort: the Mumble port as clients reach it (the apps send voice
	// to it over UDP); default Listen's port, 0: none (voice stays in the
	// WebSocket).
	PublicPort int
	ServerName string // the root channel's name
	Welcome    string
	Password   string // server password for guests
	MaxUsers   int
	MaxPerIP   int    // 0: no limit
	CertDir    string // the server certificate (made on the first start)
	// CertFile and KeyFile: a certificate from elsewhere (for example the
	// reverse proxy's Let's Encrypt files), reloaded when they change.
	CertFile, KeyFile string
	// WHIPPort: the UDP port for streams from OBS (WHIP); 0: off.
	// WHIPPublicIP: the address in ICE candidates (an IP, or a name resolved
	// at start), needed where the server does not see its public IP (Docker).
	WHIPPort     int
	WHIPPublicIP string
	// Channel recordings: where they wait for download (RecordingKeep),
	// and the limits of one.
	RecordingDir      string
	RecordingMax      time.Duration
	RecordingMaxBytes int64
	RecordingKeep     time.Duration
}

func parseBuiltin(getenv func(string) string, dbPath string) (*Builtin, error) {
	b := &Builtin{
		Listen:       cmp.Or(getenv("MUMBLE_LISTEN"), ":64738"),
		ServerName:   cmp.Or(getenv("SERVER_NAME"), "Kallisti"),
		Welcome:      getenv("MUMBLE_WELCOME"),
		Password:     getenv("MUMBLE_PASSWORD"),
		CertDir:      cmp.Or(getenv("CERT_DIR"), filepath.Dir(dbPath)),
		CertFile:     getenv("MUMBLE_CERT_FILE"),
		KeyFile:      getenv("MUMBLE_KEY_FILE"),
		WHIPPublicIP: getenv("WHIP_PUBLIC_IP"),
	}
	if (b.CertFile == "") != (b.KeyFile == "") {
		return nil, errors.New("set both MUMBLE_CERT_FILE and MUMBLE_KEY_FILE, or neither")
	}
	_, listenPort, err := net.SplitHostPort(b.Listen)
	if err != nil {
		return nil, errors.New("MUMBLE_LISTEN must be host:port")
	}
	var ok bool
	if public := getenv("MUMBLE_PUBLIC_PORT"); public != "0" {
		if b.PublicPort, ok = parsePort(cmp.Or(public, listenPort), 0); !ok {
			return nil, errors.New("MUMBLE_PUBLIC_PORT must be a port number (0: none)")
		}
	}
	if b.WHIPPort, ok = parsePort(getenv("WHIP_UDP_PORT"), 0); !ok {
		return nil, errors.New("WHIP_UDP_PORT must be a port number")
	}
	if b.MaxUsers, ok = positive(getenv("MUMBLE_MAX_USERS"), 100); !ok {
		return nil, errors.New("MUMBLE_MAX_USERS must be a positive integer")
	}
	if v := getenv("MUMBLE_MAX_PER_IP"); v != "" {
		if b.MaxPerIP, ok = positive(v, 0); !ok {
			return nil, errors.New("MUMBLE_MAX_PER_IP must be a positive integer")
		}
	}
	b.RecordingDir = cmp.Or(getenv("RECORDING_DIR"), filepath.Join(filepath.Dir(dbPath), "recordings"))
	minutes, ok := positive(getenv("RECORDING_MAX_MINUTES"), 240)
	if !ok {
		return nil, errors.New("RECORDING_MAX_MINUTES must be a positive integer")
	}
	mb, ok := positive(getenv("RECORDING_MAX_MB"), 1024)
	if !ok {
		return nil, errors.New("RECORDING_MAX_MB must be a positive integer")
	}
	days, ok := positive(getenv("RECORDING_KEEP_DAYS"), 7)
	if !ok {
		return nil, errors.New("RECORDING_KEEP_DAYS must be a positive integer")
	}
	b.RecordingMax, b.RecordingKeep = time.Duration(minutes)*time.Minute, time.Duration(days)*24*time.Hour
	b.RecordingMaxBytes = int64(mb) << 20
	return b, nil
}

// Parse reads the settings. getenv is os.Getenv in production.
func Parse(getenv func(string) string) (Config, error) {
	c := Config{MumbleHost: getenv("MUMBLE_HOST"), RejectUnauthorized: getenv("MUMBLE_INSECURE_TLS") != "1"}
	c.AllowedOrigins = list(getenv("ALLOWED_ORIGINS"))
	if len(c.AllowedOrigins) == 0 {
		return Config{}, errors.New("ALLOWED_ORIGINS is required (use * to allow every origin)")
	}
	var ok bool
	if c.MumblePort, ok = parsePort(getenv("MUMBLE_PORT"), 64738); !ok {
		return Config{}, errors.New("MUMBLE_PORT must be a port number")
	}
	if c.ListenPort, ok = parsePort(getenv("LISTEN_PORT"), 8080); !ok {
		return Config{}, errors.New("LISTEN_PORT must be a port number")
	}
	if c.MaxConnections, ok = positive(getenv("MAX_CONNECTIONS"), 100); !ok {
		return Config{}, errors.New("MAX_CONNECTIONS must be a positive integer")
	}
	if c.MaxPerIP, ok = positive(getenv("MAX_CONNECTIONS_PER_IP"), 10); !ok {
		return Config{}, errors.New("MAX_CONNECTIONS_PER_IP must be a positive integer")
	}
	c.TrustForwardedFor = getenv("TRUST_FORWARDED_FOR") == "1"
	c.NoLinkPreviews = getenv("LINK_PREVIEWS") == "0"
	c.NoFediLogin = getenv("FEDI_LOGIN") == "0"
	c.FediInsecureDev = getenv("FEDI_INSECURE_DEV_INSTANCES") == "1"
	c.NoMediaPreviews = getenv("MEDIA_PREVIEWS") == "0"
	c.ExperimentalPrivateRoomMedia = getenv("EXPERIMENTAL_PRIVATE_ROOM_MEDIA") == "1"
	if c.ExperimentalPrivateRoomMedia && c.MumbleHost != "" {
		return Config{}, errors.New("EXPERIMENTAL_PRIVATE_ROOM_MEDIA requires the built-in Mumble server")
	}
	c.PrivateRoomMediaNoSignedLinks = getenv("PRIVATE_ROOM_MEDIA_NO_SIGNED_LINKS") == "1"
	if c.PrivateRoomMediaNoSignedLinks && !c.ExperimentalPrivateRoomMedia {
		return Config{}, errors.New("PRIVATE_ROOM_MEDIA_NO_SIGNED_LINKS needs EXPERIMENTAL_PRIVATE_ROOM_MEDIA=1")
	}
	c.FFmpegPath = getenv("FFMPEG_PATH")
	var err error
	if c.Push, err = parsePush(getenv); err != nil {
		return Config{}, err
	}
	c.UploadDir = cmp.Or(getenv("UPLOAD_DIR"), "uploads")
	mb, ok := positive(getenv("UPLOAD_MAX_MB"), 10)
	if !ok {
		return Config{}, errors.New("UPLOAD_MAX_MB must be a positive integer")
	}
	c.UploadMaxBytes = int64(mb) << 20
	quota, ok := positive(getenv("UPLOAD_QUOTA_MB"), 100)
	if !ok {
		return Config{}, errors.New("UPLOAD_QUOTA_MB must be a positive integer")
	}
	c.UploadQuotaBytes = int64(quota) << 20
	c.BackupDir = getenv("BACKUP_DIR")
	if c.BackupKeep, ok = positive(getenv("BACKUP_KEEP"), 7); !ok {
		return Config{}, errors.New("BACKUP_KEEP must be a positive integer")
	}
	c.WebDir = getenv("WEB_DIR")
	c.DBPath = cmp.Or(getenv("DB_PATH"), "kallisti.db")
	if getenv("TRANSLATION") != "0" {
		if c.TranslationLanguages = list(getenv("TRANSLATION_LANGUAGES")); len(c.TranslationLanguages) == 0 {
			c.TranslationLanguages = []string{"de", "es", "fr", "ja", "zh", "zh-Hant"}
		}
	}
	c.TranslationDir = cmp.Or(getenv("TRANSLATION_DIR"), filepath.Join(filepath.Dir(c.DBPath), "translation-models"))
	if c.TextChannels = list(getenv("TEXT_CHANNELS")); len(c.TextChannels) == 0 {
		c.TextChannels = []string{"general"}
	}
	if c.MumbleHost == "" {
		if c.Builtin, err = parseBuiltin(getenv, c.DBPath); err != nil {
			return Config{}, err
		}
	}
	return c, nil
}

// OriginAllowed reports whether a browser origin may connect: every one.
// Pages of other sites (another Kallisti's web app with several servers,
// the desktop and mobile apps) log in with a token, never this server's
// cookie (see CookieOrigin). An empty origin (non-browser client) only
// passes with "*".
func OriginAllowed(allowed []string, origin string) bool {
	return origin != "" || slices.Contains(allowed, "*")
}

// CookieOrigin: the session cookie counts for this origin: this server's
// own pages (ALLOWED_ORIGINS), localhost with "*" (development), or none
// (a page's own GET, a script).
func CookieOrigin(allowed []string, origin string) bool {
	return origin == "" || slices.Contains(allowed, origin) || slices.Contains(allowed, "*") && IsLocalhost(origin)
}

// CORS lets a page of origin call with these methods and headers (no
// origin: not a browser's cross-site call, nothing to allow); credentials:
// also with its cookies.
func CORS(w http.ResponseWriter, origin, methods, headers string, credentials bool) {
	h := w.Header()
	h.Add("Vary", "Origin")
	if origin == "" {
		return
	}
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Methods", methods)
	h.Set("Access-Control-Allow-Headers", headers)
	if credentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
}

// appOrigins are the Kallisti desktop and mobile apps' pages (Tauri). A
// website cannot send them (browsers set Origin); a local page can (see
// auth: guests only).
var appOrigins = []string{"tauri://localhost", "http://tauri.localhost", "https://tauri.localhost"}

// OwnSite: our own pages or the apps: where guest accounts are made (not
// another site, which could make its visitors fill the server with guests).
func OwnSite(allowed []string, origin string) bool {
	return CookieOrigin(allowed, origin) || slices.Contains(appOrigins, origin)
}

// IsLocalhost: a development page on this computer (http or https: not
// the apps' tauri://localhost).
func IsLocalhost(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// BodyTimeout is how long a small request body (JSON) may take: the HTTP
// server has only a header timeout, and a proxy may pass a slow body on
// without one.
var BodyTimeout = 10 * time.Second

// LimitBody gives the request's body BodyTimeout. An early answer (the
// body unread) stops waiting for the rest of it then too.
func LimitBody(w http.ResponseWriter) {
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(BodyTimeout))
}
