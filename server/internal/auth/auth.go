// Package auth handles accounts: create, log in, log out, and finding the
// account behind a request (session cookie).
package auth

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"kallisti.chat/server/internal/password"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"golang.org/x/time/rate"

	"kallisti.chat/server/internal/certs"
	"kallisti.chat/server/internal/clientip"
	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/store"
)

const (
	CookieName      = "kallisti_session"
	sessionLifetime = 30 * 24 * time.Hour
	// Account creation and login attempts per client IP.
	perMinute = 10
	burst     = 10
	// Failed logins from other sites per account name, whatever the
	// address (guesses spread over another site's visitors): 30 an hour
	// after a burst of 10.
	accountPerHour = 30
	// No account (32) or Mumble registration has a longer name.
	maxLoginName = 128
	// Guest accounts per client IP: a few at once, then one per 10 minutes.
	guestBurst = 5
	guestEvery = 10 * time.Minute
	// Temporary accounts at the same time, by default.
	defaultMaxGuests = 1000
)

type Service struct {
	Store             *store.Store
	Now               func() time.Time
	AllowedOrigins    []string
	TrustForwardedFor bool
	// NameTaken reports names (as names.Key) used elsewhere, for example by
	// users registered on the Mumble server. Nil: none.
	NameTaken func(key string) bool
	// MumbleLogin checks a Mumble registration's password (built-in server)
	// for names without a web account. Such logins get no session: the
	// client connects with the name and password. Nil: none.
	MumbleLogin func(host netip.Addr, name, password string) (string, bool)
	// GuestAccounts: "Join as guest" makes a temporary account (the
	// built-in server, which can remove their registrations). MaxGuests:
	// temporary accounts at the same time (0: defaultMaxGuests).
	GuestAccounts bool
	MaxGuests     int
	// ServerPassword: guests may need the Mumble server's password (Murmur:
	// unknown, so true). The login page leaves out its field otherwise.
	ServerPassword bool
	// VoicePort: the built-in server's public Mumble port; the apps send
	// voice to it over UDP (0: none, voice stays in the WebSocket).
	VoicePort int
	// ServerName: the server's name for the login page (nil: not known,
	// with Murmur).
	ServerName func() string
	// Revoke closes the open connections whose account ID and session
	// (SessionKey) match, when sessions end. Nil: none.
	Revoke func(match func(accountID int64, session string) bool)
	// EndPasswordLogins disconnects the Mumble clients that logged in with
	// the account's password, after a password change or a logout
	// everywhere. Nil: none.
	EndPasswordLogins func(acc store.Account)
	// OnPermanent hears that a guest account became a normal account (by
	// name). Nil: none.
	OnPermanent func(name string)
	// Captcha: the admins' level for new accounts and guest accounts (see
	// challenge.go). Nil: none.
	Captcha func() string
	// StreamKeys: streaming from OBS is on (see streamkey.go).
	StreamKeys bool
	// StreamKeyChanged hears that an account's stream key was replaced or
	// removed (streams with the old one end). Nil: none.
	StreamKeyChanged func(name string)
	// Fedi: logins with fediverse accounts (see fedi.go). Nil: off.
	Fedi *FediLogin
	// Provider: other Kallisti servers log in with accounts here (see
	// provider.go); nil: off.
	Provider *Provider
	// SetAvatar gives an account an avatar (a JPEG, see avatarimg), only
	// if it has none if onlyIfNone. Nil: none.
	SetAvatar func(acc store.Account, avatar []byte, onlyIfNone bool)

	logins, guests limiter
	qr             qrGrants // see qr.go
	failures       failures
	challenges     challenges
}

// limiter keeps a rate limiter per client IP.
type limiter struct {
	mu sync.Mutex
	m  map[string]*rate.Limiter
}

func (l *limiter) allow(ip string, limit rate.Limit, burst int) bool {
	key := clientip.Key(ip)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]*rate.Limiter{}
	}
	lim, ok := l.m[key]
	if !ok {
		if len(l.m) > 10_000 {
			// Keep memory bounded: drop idle limiters (full again), not all.
			for k, v := range l.m {
				if v.Tokens() >= float64(burst) {
					delete(l.m, k)
				}
			}
		}
		lim = rate.NewLimiter(limit, burst)
		l.m[key] = lim
	}
	return lim.Allow()
}

// failures counts failed logins per account name (hashed: names come from
// anyone), for logins from other sites. An attempt takes a token before the
// password work (parallel guesses count at once); a good login gives it
// back, so a user's own logins do not use up the budget.
type failures struct {
	mu sync.Mutex
	m  map[[16]byte]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

const maxFailureNames = 10_000

func failureKey(name string) [16]byte {
	sum := sha256.Sum256([]byte(names.Key(name)))
	return [16]byte(sum[:16])
}

func (b *bucket) refill(now time.Time) {
	b.tokens = min(burst, b.tokens+now.Sub(b.at).Hours()*accountPerHour)
	b.at = now
}

// take: one attempt for this name; false: too many lately for it, or (full)
// a new name while every name is counting.
func (f *failures) take(name string, now time.Time) (ok, full bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[[16]byte]*bucket{}
	}
	key := failureKey(name)
	b := f.m[key]
	if b == nil {
		if !f.room(now) {
			return false, true
		}
		b = &bucket{tokens: burst, at: now}
		f.m[key] = b
	}
	b.refill(now)
	if b.tokens < 1 {
		return false, false
	}
	b.tokens--
	return true, false
}

// refund: the attempt was no failure.
func (f *failures) refund(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b := f.m[failureKey(name)]; b != nil {
		b.tokens = min(burst, b.tokens+1)
	}
}

// room keeps the map bounded: only names that are full again go (one
// still counting is never dropped, or many other names could reset a
// blocked account). False: every name is counting; a new one waits (only
// logins from other sites, so nobody is locked out of our own).
func (f *failures) room(now time.Time) bool {
	if len(f.m) < maxFailureNames {
		return true
	}
	for k, b := range f.m {
		if b.refill(now); b.tokens >= burst {
			delete(f.m, k)
		}
	}
	return len(f.m) < maxFailureNames
}

// ownSite: new accounts and guests only on our own pages and in the apps,
// else any site could make its visitors fill the server; it answers with
// why not.
func (a *Service) ownSite(w http.ResponseWriter, r *http.Request, why string) bool {
	if config.OwnSite(a.AllowedOrigins, r.Header.Get("Origin")) {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": why})
	return false
}

type credentials struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Invite   string `json:"invite"` // account invitation, never a channel invite
	// A solved challenge (see challenge.go), for new accounts.
	Challenge string `json:"challenge"`
	Nonce     string `json:"nonce"`
}

// Paths: what ServeHTTP answers (for the server's router).
var Paths = []string{"/api/server", "/api/challenge", "/api/accounts", "/api/guest", "/api/login", "/api/logout", "/api/logout-all", "/api/password", "/api/me", "/api/servers", "/api/friends", "/api/stream-key", "/api/fedi", "/api/fedi/",
	"/api/v1/", "/oauth/", "/.well-known/webfinger", "/api/oauth/", "/api/qr/"}

func (a *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Other servers' requests: forms, no cookies (see provider.go).
	if a.serveProvider(w, r) {
		return
	}
	if r.Method == http.MethodDelete {
		config.LimitBody(w)
	}
	if r.Method == http.MethodPost {
		config.LimitBody(w)
		// Against cross-site forms (CSRF): a form cannot send JSON without a
		// CORS preflight, and browsers mark cross-site requests.
		ctype, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if ctype != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "send JSON"})
			return
		}
		// Other sites (another Kallisti's page, the apps) are cross-site by
		// nature and log in with a token: never with the cookie.
		tokens := !config.CookieOrigin(a.AllowedOrigins, r.Header.Get("Origin")) && a.cookie(r, CookieName) == ""
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" && !tokens {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site requests are not allowed"})
			return
		}
	}
	if a.serveFedi(w, r) {
		return
	}
	switch {
	case r.URL.Path == "/api/server" && r.Method == http.MethodGet:
		info := map[string]any{"password": a.ServerPassword}
		if on, err := a.Store.InviteOnly(); err == nil {
			info["inviteOnly"] = on // else left out: the signup form finds out on submit
		}
		if a.ServerName != nil {
			info["name"] = a.ServerName()
		}
		if v, err := a.Store.LogoVersion(); err == nil && v != "" {
			info["logo"] = v // at /api/logo/<size>.png?v=<version>
		}
		if a.VoicePort != 0 {
			info["udpPort"] = a.VoicePort
		}
		writeJSON(w, http.StatusOK, info)
	case r.URL.Path == "/api/challenge" && r.Method == http.MethodGet:
		a.challenge(w)
	case r.URL.Path == "/api/accounts" && r.Method == http.MethodPost:
		a.create(w, r)
	case r.URL.Path == "/api/guest" && r.Method == http.MethodPost && a.GuestAccounts:
		a.guest(w, r)
	case r.URL.Path == "/api/login" && r.Method == http.MethodPost:
		a.login(w, r)
	case r.URL.Path == "/api/logout" && r.Method == http.MethodPost:
		a.logout(w, r)
	case r.URL.Path == "/api/logout-all" && r.Method == http.MethodPost:
		a.logoutAll(w, r)
	case r.URL.Path == "/api/password" && r.Method == http.MethodPost:
		a.changePassword(w, r)
	case r.URL.Path == "/api/qr/start" && r.Method == http.MethodPost:
		a.qrStart(w, r)
	case r.URL.Path == "/api/qr/scan" && r.Method == http.MethodPost:
		a.qrScan(w, r)
	case r.URL.Path == "/api/qr/answer" && r.Method == http.MethodPost:
		a.qrAnswer(w, r)
	case r.URL.Path == "/api/qr/wait" && r.Method == http.MethodPost:
		a.qrWait(w, r)
	case r.URL.Path == "/api/oauth/request" && r.Method == http.MethodGet && a.Provider != nil:
		a.oauthRequest(w, r)
	case r.URL.Path == "/api/oauth/approve" && r.Method == http.MethodPost && a.Provider != nil:
		a.oauthApprove(w, r)
	case r.URL.Path == "/api/oauth/grants" && (r.Method == http.MethodGet || r.Method == http.MethodDelete) && a.Provider != nil:
		a.oauthGrants(w, r)
	case r.URL.Path == "/api/servers":
		a.servers(w, r)
	case r.URL.Path == "/api/friends":
		a.friends(w, r)
	case r.URL.Path == "/api/stream-key":
		a.streamKey(w, r)
	case r.URL.Path == "/api/me" && r.Method == http.MethodGet:
		if acc, ok := a.AccountFor(r); ok {
			if token, plain := a.sessionCookie(r); plain && token == a.sessionToken(r) {
				a.setSessionCookie(w, r, token)
				http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
			}
			me := map[string]any{"name": acc.Name, "temporary": acc.Temporary, "password": acc.PasswordHash != "", "session": sessionID(a.SessionKey(r))}
			if f := a.fediMe(acc); f != nil {
				me["fedi"] = f
			}
			writeJSON(w, http.StatusOK, me)
		} else {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		}
	default:
		http.NotFound(w, r)
	}
}

// sessionToken is a request's session: the cookie (web app), else the
// token the desktop app got at login, in a header or (WebSockets cannot
// have headers in browsers) as a subprotocol. "": none.
// A token comes first: a leftover cookie must not win over it.
func (a *Service) sessionToken(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return token
	}
	for _, p := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if token, ok := strings.CutPrefix(strings.TrimSpace(p), SessionProtocol); ok {
			return token
		}
	}
	// Other pages use tokens: the cookie counts only without an origin (a
	// navigation, a page's own GET) or from our own origins, not from a
	// sibling subdomain (the same site: its requests carry the cookie).
	if !config.CookieOrigin(a.AllowedOrigins, r.Header.Get("Origin")) {
		return ""
	}
	token, _ := a.sessionCookie(r)
	return token
}

// plainCookiesUntil: sessions from before the __Host- cookies (deployed
// 2026-10-28) count with their plain-named cookie over HTTPS until then
// (sessions last 30 days: one made after the deploy ends later), and
// /api/me moves them to the new name, so nobody has to log in again.
// Remove after the date.
var plainCookiesUntil = time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)

// sessionCookie is the session cookie's value; plain: the old name.
func (a *Service) sessionCookie(r *http.Request) (string, bool) {
	if token := a.cookie(r, CookieName); token != "" {
		return token, false
	}
	// Only a session from before (it ends by then: sessions are not
	// extended): a sibling subdomain cannot plant a new one.
	if a.secure(r) && a.Now().Before(plainCookiesUntil) {
		if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
			if ends, err := a.Store.SessionExpires(tokenHash(c.Value)); err == nil && !ends.After(plainCookiesUntil) {
				return c.Value, true
			}
		}
	}
	return "", false
}

// TokenEnded: the request brings a session token (header or subprotocol,
// not a cookie) that is no session (any more): expired, or ended. Not on
// other errors (a busy database): those are worth another try.
func (a *Service) TokenEnded(r *http.Request) bool {
	token := a.sessionToken(r)
	_, hasBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || !(hasBearer || strings.Contains(r.Header.Get("Sec-WebSocket-Protocol"), SessionProtocol)) {
		return false
	}
	_, err := a.Store.AccountBySession(tokenHash(token), a.Now())
	return errors.Is(err, store.ErrNotFound)
}

// SessionProtocol: the desktop app's WebSocket subprotocol with its token.
const SessionProtocol = "kallisti.session."

// AccountFor returns the account of a request's session.
func (a *Service) AccountFor(r *http.Request) (store.Account, bool) {
	token := a.sessionToken(r)
	if token == "" {
		return store.Account{}, false
	}
	acc, err := a.Store.AccountBySession(tokenHash(token), a.Now())
	return acc, err == nil
}

// SessionKey is the stored key of a request's session, or "".
func (a *Service) SessionKey(r *http.Request) string {
	if token := a.sessionToken(r); token != "" {
		return tokenHash(token)
	}
	return ""
}

func (a *Service) revoke(match func(accountID int64, session string) bool) {
	if a.Revoke != nil {
		a.Revoke(match)
	}
}

// endLogins ends the account's sessions with end, then what lives on of
// its logins but the session keep ("": none). The store change and handoff
// revocation are serialized with handoff redemption; hooks run unlocked.
func (a *Service) endLogins(acc store.Account, keep string, end func() error) error {
	if err := a.revokeHandoffs(acc.ID, end); err != nil {
		return err
	}
	a.revoke(func(id int64, session string) bool { return id == acc.ID && (keep == "" || session != keep) })
	a.endStreamKey(acc)
	a.endPasswordLogins(acc)
	return nil
}

// DeleteAccount deletes an account (an admin removes its registration):
// its sessions go with it, and its logins end. Its messages stay.
func (a *Service) DeleteAccount(acc store.Account) error {
	return a.endLogins(acc, "", func() error { return a.Store.DeleteAccount(acc.ID) })
}

func (a *Service) endPasswordLogins(acc store.Account) {
	if a.EndPasswordLogins != nil {
		a.EndPasswordLogins(acc)
	}
}

// DeleteExpiredSessions ends the expired sessions and their connections.
func (a *Service) DeleteExpiredSessions() error {
	if a.Provider != nil {
		if err := a.Store.DeleteExpiredOAuthTokens(a.Now()); err != nil {
			return err
		}
	}
	n, err := a.Store.DeleteExpiredSessions(a.Now())
	if err != nil || n == 0 {
		return err
	}
	a.revoke(func(_ int64, session string) bool {
		_, err := a.Store.AccountBySession(session, a.Now())
		return errors.Is(err, store.ErrNotFound)
	})
	return nil
}

func (a *Service) create(w http.ResponseWriter, r *http.Request) {
	if !a.ownSite(w, r, "create an account on the server's own site") {
		return
	}
	if !a.allow(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var c credentials
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if !a.mayRegister(w, c.Invite, false) {
		return
	}
	name, ok := a.checkName(w, c.Name)
	if !ok {
		return
	}
	if n := utf8.RuneCountInString(c.Password); n < 8 || n > 256 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the password needs 8 to 256 characters"})
		return
	}
	// After the quick checks (a taken name does not use up the challenge),
	// before the slow hash.
	if !a.proven(w, c) {
		return
	}
	release := password.Limit()
	hash, err := password.Hash(c.Password)
	release()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create the account"})
		return
	}
	if acc, token, ok := a.newAccount(w, r, store.Account{Name: name, PasswordHash: hash}, true, func(acc store.Account) (store.Account, error) {
		return a.Store.CreateAccount(acc, a.Now(), c.Invite)
	}); ok {
		writeJSON(w, http.StatusCreated, withToken(map[string]any{"name": acc.Name}, token))
	}
}

// guest makes a temporary account: no password, kept by its session
// cookie, deleted after a day without a visit.
func (a *Service) guest(w http.ResponseWriter, r *http.Request) {
	if !a.mayRegister(w, "", true) {
		return
	}
	if !a.ownSite(w, r, "join as a guest on the server's own site") {
		return
	}
	if !a.allow(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var c credentials
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	name, ok := a.checkName(w, c.Name)
	if !ok || !a.proven(w, c) {
		return
	}
	if n, err := a.Store.TemporaryAccounts(); err != nil || n >= cmp.Or(a.MaxGuests, defaultMaxGuests) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "too many guests right now, please create an account"})
		return
	}
	// Only now (a name that can be used): taken names do not use it up.
	if !a.guests.allow(clientip.Of(r, a.TrustForwardedFor), rate.Every(guestEvery), guestBurst) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many guests from your address, try again later"})
		return
	}
	if acc, token, ok := a.newAccount(w, r, store.Account{Name: name, Temporary: true}, true, a.createAccount); ok {
		writeJSON(w, http.StatusCreated, withToken(map[string]any{"name": acc.Name, "temporary": true}, token))
	}
}

// checkName answers the request if a new account may not have the name.
func (a *Service) checkName(w http.ResponseWriter, raw string) (string, bool) {
	return a.checkNameBy(w, raw, names.Check, http.StatusBadRequest)
}

// checkRemoteName: an automatic name from another server. An unusable
// name is a recoverable conflict: keep the remote proof and ask for a local name.
func (a *Service) checkRemoteName(w http.ResponseWriter, raw string) (string, bool) {
	return a.checkNameBy(w, raw, names.CheckRemote, http.StatusConflict)
}

func (a *Service) checkNameBy(w http.ResponseWriter, raw string, check func(string) string, invalidStatus int) (string, bool) {
	name := norm.NFC.String(strings.TrimSpace(raw))
	if msg := check(name); msg != "" {
		writeJSON(w, invalidStatus, map[string]string{"error": msg})
		return "", false
	}
	_, err := a.Store.AccountByName(name)
	if err == nil || a.NameTaken != nil && a.NameTaken(names.Key(name)) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this name is already taken"})
		return "", false
	}
	return name, true
}

func (a *Service) createAccount(acc store.Account) (store.Account, error) {
	return a.Store.CreateAccount(acc, a.Now(), "")
}

// newAccount stores an account with a new certificate (by create) and logs
// it in; on failure it answers the request (session: with a session; a
// handoff makes its own, see fediHandoff).
func (a *Service) newAccount(w http.ResponseWriter, r *http.Request, acc store.Account, session bool, create func(store.Account) (store.Account, error)) (store.Account, string, bool) {
	fail := func() (store.Account, string, bool) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create the account"})
		return store.Account{}, "", false
	}
	var err error
	if acc.CertPEM, acc.KeyPEM, err = certs.Generate(acc.Name); err != nil {
		return fail()
	}
	acc, err = create(acc)
	if registrationError(w, err) {
		return store.Account{}, "", false
	}
	if errors.Is(err, store.ErrNameTaken) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this name is already taken"})
		return store.Account{}, "", false
	}
	if errors.Is(err, store.ErrFediTaken) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this fediverse account has a Kallisti account already: log in with it"})
		return store.Account{}, "", false
	}
	if err != nil {
		slog.Error("create account", "err", err)
		return fail()
	}
	if !session {
		return acc, "", true
	}
	token, ok := a.startSession(w, r, acc)
	return acc, token, ok
}

func (a *Service) login(w http.ResponseWriter, r *http.Request) {
	if !a.allow(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var c credentials
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	// Names longer than any account's: wrong at once, not counted.
	if utf8.RuneCountInString(c.Name) > maxLoginName {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong name or password"})
		return
	}
	// From other sites: a limit per account (guesses spread over their
	// visitors' addresses); our own pages and the apps keep the limit per
	// address only, so nobody outside can lock an account.
	guarded := !config.OwnSite(a.AllowedOrigins, r.Header.Get("Origin"))
	if guarded {
		if ok, full := a.failures.take(c.Name, a.Now()); !ok {
			why := "too many attempts for this name, try again later"
			if full {
				why = "too many logins from other sites right now, try again later or on the server's own site"
			}
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": why})
			return
		}
	}
	acc, err := a.Store.AccountByName(c.Name)
	// From other sites, a name without a web account gives its token back
	// (random names cannot fill the table) and is answered before the
	// password, so the answer says nothing about the password (that the
	// name has no web account shows, as signing up shows "taken"): wrong,
	// or (with the built-in server) maybe a Mumble registration, whose
	// socket uses the name and password, which other sites cannot (theirs
	// need a token).
	if guarded && errors.Is(err, store.ErrNotFound) {
		a.failures.refund(c.Name)
		if a.MumbleLogin != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "log in with a Mumble registration on the server's own site"})
		} else {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong name or password"})
		}
		return
	}
	hash := acc.PasswordHash
	if err != nil || acc.Temporary || hash == "" {
		hash = password.Dummy // same work for unknown names; no login without a password
	}
	release := password.Limit()
	ok := password.Check(hash, c.Password)
	release()
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		if guarded {
			a.failures.refund(c.Name) // our fault, no failed guess
		}
		slog.Error("login", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "please try again later"})
		return
	}
	if err != nil && a.MumbleLogin != nil {
		host, _ := netip.ParseAddr(clientip.Of(r, a.TrustForwardedFor))
		if name, ok := a.MumbleLogin(host.Unmap(), c.Name, c.Password); ok {
			// Not a web account: end a web session of this browser, or the
			// bridge would connect with that account instead.
			a.clearSession(w, r)
			writeJSON(w, http.StatusOK, map[string]any{"name": name, "mumble": true})
			return
		}
	}
	if !ok || err != nil || acc.Temporary || acc.PasswordHash == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong name or password"})
		return
	}
	if guarded {
		a.failures.refund(c.Name)
	}
	token, ok := a.startSession(w, r, acc)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, withToken(map[string]any{"name": acc.Name}, token))
}

// logout ends this session. A guest account cannot be reached again
// without it: it ends too (the cleanup removes it at its next round), so
// its name is free soon.
func (a *Service) logout(w http.ResponseWriter, r *http.Request) {
	// It may name the session it means (see sessionID): a login screen that
	// still shows an older login must not end the one now in this browser.
	// It may also name the account (a guest has no fingerprint).
	var body struct {
		Session string `json:"session"`
		Name    string `json:"name"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	acc, ok := a.AccountFor(r)
	key := a.SessionKey(r)
	if (body.Session != "" && key != "" && body.Session != sessionID(key)) || (body.Name != "" && ok && names.Key(body.Name) != names.Key(acc.Name)) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "another login is active here now"})
		return
	}
	if ok && acc.Temporary {
		if err := a.Store.ExpireAccount(acc.ID); err != nil {
			slog.Error("ending a guest account", "account", acc.Name, "err", err)
		}
	}
	a.clearSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// clearSession ends this session and its connections.
func (a *Service) clearSession(w http.ResponseWriter, r *http.Request) {
	if key := a.SessionKey(r); key != "" {
		a.Store.DeleteSession(key)
		a.revoke(func(_ int64, session string) bool { return session == key })
	}
	a.clearCookie(w, r, CookieName)
}

// logoutAll ends every session of the account, this one too.
func (a *Service) logoutAll(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.AccountFor(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}
	if err := a.endLogins(acc, "", func() error { return a.Store.DeleteSessionsExcept(acc.ID, "") }); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot log out"})
		return
	}
	a.clearCookie(w, r, CookieName)
	w.WriteHeader(http.StatusNoContent)
}

// changePassword needs the old password and ends all other sessions.
func (a *Service) changePassword(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.AccountFor(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}
	if !a.allow(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var c struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if n := utf8.RuneCountInString(c.New); n < 8 || n > 256 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the password needs 8 to 256 characters"})
		return
	}
	// A temporary account has no password yet: the first one makes it a
	// normal account. An account made with a fediverse login gets its first
	// one too (for Mumble apps, or to unlink), after a fresh login with its
	// fediverse account in this session: a stolen session cannot set one and
	// lock the owner out (the change ends the other sessions). With
	// fediverse logins off there is no other way back in: no check then.
	if acc.PasswordHash == "" && !acc.Temporary && a.Fedi != nil && !a.Fedi.takeProof(a.SessionKey(r), acc.ID, a.Now()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "confirm with your fediverse account first", "code": "reauth"})
		return
	}
	release := password.Limit()
	ok = acc.Temporary || acc.PasswordHash == "" || password.Check(acc.PasswordHash, c.Old)
	var hash string
	var err error
	if ok {
		hash, err = password.Hash(c.New)
	}
	release()
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "the current password is wrong"})
		return
	}
	// acc is from before the body was read: the store checks again that
	// the session and the old password are still current.
	keep := a.SessionKey(r)
	if err == nil {
		err = a.endLogins(acc, keep, func() error {
			return a.Store.ChangePassword(acc.ID, keep, acc.PasswordHash, hash, a.Now())
		})
	}
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "the password or session changed meanwhile"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot change the password"})
		return
	}
	if acc.Temporary && a.OnPermanent != nil {
		a.OnPermanent(acc.Name)
	}
	w.WriteHeader(http.StatusNoContent)
}

// startSession sets the cookie; the token for clients that asked for it
// (the desktop app, header X-Kallisti-Session: token), else "".
func (a *Service) startSession(w http.ResponseWriter, r *http.Request, acc store.Account) (string, bool) {
	token, err := a.newSession(acc)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot log in"})
		return "", false
	}
	return a.handOutSession(w, r, token), true
}

// handOutSession sets a new session's cookie; its token for clients that
// asked for it, else "".
func (a *Service) handOutSession(w http.ResponseWriter, r *http.Request, token string) string {
	a.setSessionCookie(w, r, token)
	if r.Header.Get("X-Kallisti-Session") != "token" {
		return ""
	}
	return token
}

// newSession is a new session's token.
func (a *Service) newSession(acc store.Account) (string, error) {
	token := randomHex(32)
	return token, a.Store.CreateSession(tokenHash(token), acc.ID, a.Now().Add(sessionLifetime))
}

func (a *Service) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName(r, CookieName),
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   a.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// cookieName: with HTTPS the __Host- prefix: browsers take such a cookie
// only from this host itself (Secure, Path=/, no Domain), so a sibling
// subdomain cannot plant one (a session or a fediverse flow of its own).
func (a *Service) cookieName(r *http.Request, base string) string {
	if a.secure(r) {
		return "__Host-" + base
	}
	return base
}

// cookie is the value of our cookie of this name ("": none).
func (a *Service) cookie(r *http.Request, base string) string {
	if c, err := r.Cookie(a.cookieName(r, base)); err == nil {
		return c.Value
	}
	return ""
}

// clearCookie removes it, and one of the plain name from before.
func (a *Service) clearCookie(w http.ResponseWriter, r *http.Request, base string) {
	for _, name := range []string{a.cookieName(r, base), base} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure(r), SameSite: http.SameSiteLaxMode})
	}
}

func (a *Service) secure(r *http.Request) bool {
	return r.TLS != nil || (a.TrustForwardedFor && r.Header.Get("X-Forwarded-Proto") == "https")
}

// cors allows every site to call the API with a token, and the web app's
// own origin with its cookie. With ALLOWED_ORIGINS=* (development) only
// localhost origins get credentials: a production server set to * by
// mistake is not open to every site's cookie requests.
func (a *Service) cors(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	config.CORS(w, origin, "GET, POST, DELETE", "Content-Type, Authorization, X-Kallisti-Session", config.CookieOrigin(a.AllowedOrigins, origin))
}

func (a *Service) allow(r *http.Request) bool {
	return a.logins.allow(clientip.Of(r, a.TrustForwardedFor), rate.Limit(perMinute)/60, burst)
}

// sessionID: a short name of a session (in /api/me) that a logout can
// name, so a page showing an older login does not end a newer one; derived
// from its key, not the key.
func sessionID(key string) string {
	sum := sha256.Sum256([]byte("session-id:" + key))
	return hex.EncodeToString(sum[:8])
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// withToken adds the session token for the desktop app (see startSession).
func withToken(v map[string]any, token string) map[string]any {
	if token != "" {
		v["token"] = token
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
