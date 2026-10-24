// Package auth handles accounts: create, log in, log out, and finding the
// account behind a request (session cookie).
package auth

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"kallisti.chat/server/internal/password"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
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
	// NoRegistration: no new accounts, by any way (create, fediverse
	// signup, a guest account kept with a password or a link). Guest
	// accounts and logins stay.
	NoRegistration bool
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
	// SetAvatar gives an account an avatar (a JPEG, see avatarimg), only
	// if it has none if onlyIfNone. Nil: none.
	SetAvatar func(acc store.Account, avatar []byte, onlyIfNone bool)

	logins, guests limiter
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

type credentials struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	// A solved challenge (see challenge.go), for new accounts.
	Challenge string `json:"challenge"`
	Nonce     string `json:"nonce"`
}

func (a *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
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
		// The desktop app is cross-site by nature, and never sends the
		// cookie (a local page can pose as http://tauri.localhost).
		_, noCookie := r.Cookie(CookieName)
		app := config.IsAppOrigin(r.Header.Get("Origin")) && noCookie != nil
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" && !app {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site requests are not allowed"})
			return
		}
	}
	if a.serveFedi(w, r) {
		return
	}
	switch {
	case r.URL.Path == "/api/server" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]bool{"password": a.ServerPassword, "registration": !a.NoRegistration})
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
	case r.URL.Path == "/api/stream-key":
		a.streamKey(w, r)
	case r.URL.Path == "/api/me" && r.Method == http.MethodGet:
		if acc, ok := a.AccountFor(r); ok {
			me := map[string]any{"name": acc.Name, "temporary": acc.Temporary, "password": acc.PasswordHash != ""}
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
func sessionToken(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return token
	}
	for _, p := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if token, ok := strings.CutPrefix(strings.TrimSpace(p), SessionProtocol); ok {
			return token
		}
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return ""
}

// SessionProtocol: the desktop app's WebSocket subprotocol with its token.
const SessionProtocol = "kallisti.session."

// AccountFor returns the account of a request's session.
func (a *Service) AccountFor(r *http.Request) (store.Account, bool) {
	token := sessionToken(r)
	if token == "" {
		return store.Account{}, false
	}
	acc, err := a.Store.AccountBySession(tokenHash(token), a.Now())
	return acc, err == nil
}

// SessionKey is the stored key of a request's session, or "".
func SessionKey(r *http.Request) string {
	if token := sessionToken(r); token != "" {
		return tokenHash(token)
	}
	return ""
}

func (a *Service) revoke(match func(accountID int64, session string) bool) {
	if a.Revoke != nil {
		a.Revoke(match)
	}
}

func (a *Service) endPasswordLogins(acc store.Account) {
	if a.EndPasswordLogins != nil {
		a.EndPasswordLogins(acc)
	}
}

// DeleteExpiredSessions ends the expired sessions and their connections.
func (a *Service) DeleteExpiredSessions() error {
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

// registrationClosed answers the request if the server takes no new
// accounts.
func (a *Service) registrationClosed(w http.ResponseWriter) bool {
	if a.NoRegistration {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": registrationClosedText})
	}
	return a.NoRegistration
}

const registrationClosedText = "this server does not take new accounts"

func (a *Service) create(w http.ResponseWriter, r *http.Request) {
	if a.registrationClosed(w) {
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
	if acc, token, ok := a.newAccount(w, r, store.Account{Name: name, PasswordHash: hash}, a.createAccount); ok {
		writeJSON(w, http.StatusCreated, withToken(map[string]any{"name": acc.Name}, token))
	}
}

// guest makes a temporary account: no password, kept by its session
// cookie, deleted after a day without a visit.
func (a *Service) guest(w http.ResponseWriter, r *http.Request) {
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
	if acc, token, ok := a.newAccount(w, r, store.Account{Name: name, Temporary: true}, a.createAccount); ok {
		writeJSON(w, http.StatusCreated, withToken(map[string]any{"name": acc.Name, "temporary": true}, token))
	}
}

// checkName answers the request if a new account may not have the name.
func (a *Service) checkName(w http.ResponseWriter, raw string) (string, bool) {
	name := norm.NFC.String(strings.TrimSpace(raw))
	if msg := names.Check(name); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
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
	return a.Store.CreateAccount(acc, a.Now())
}

// newAccount stores an account with a new certificate (by create) and logs
// it in; on failure it answers the request.
func (a *Service) newAccount(w http.ResponseWriter, r *http.Request, acc store.Account, create func(store.Account) (store.Account, error)) (store.Account, string, bool) {
	fail := func() (store.Account, string, bool) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create the account"})
		return store.Account{}, "", false
	}
	var err error
	if acc.CertPEM, acc.KeyPEM, err = certs.Generate(acc.Name); err != nil {
		return fail()
	}
	acc, err = create(acc)
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
	acc, err := a.Store.AccountByName(c.Name)
	hash := acc.PasswordHash
	if err != nil || acc.Temporary || hash == "" {
		hash = password.Dummy // same work for unknown names; no login without a password
	}
	release := password.Limit()
	ok := password.Check(hash, c.Password)
	release()
	if err != nil && !errors.Is(err, store.ErrNotFound) {
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
	if acc, ok := a.AccountFor(r); ok && acc.Temporary {
		if err := a.Store.ExpireAccount(acc.ID); err != nil {
			slog.Error("ending a guest account", "account", acc.Name, "err", err)
		}
	}
	a.clearSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// clearSession ends this session and its connections.
func (a *Service) clearSession(w http.ResponseWriter, r *http.Request) {
	if key := SessionKey(r); key != "" {
		a.Store.DeleteSession(key)
		a.revoke(func(_ int64, session string) bool { return session == key })
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// logoutAll ends every session of the account, this one too.
func (a *Service) logoutAll(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.AccountFor(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}
	if err := a.Store.DeleteSessionsExcept(acc.ID, ""); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot log out"})
		return
	}
	a.revoke(func(id int64, _ string) bool { return id == acc.ID })
	a.endStreamKey(acc)
	a.endPasswordLogins(acc)
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

// changePassword needs the old password and ends all other sessions.
func (a *Service) changePassword(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.AccountFor(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}
	// A guest's first password keeps the account: a new account.
	if acc.Temporary && a.registrationClosed(w) {
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
	if acc.PasswordHash == "" && !acc.Temporary && a.Fedi != nil && !a.Fedi.takeProof(SessionKey(r), acc.ID, a.Now()) {
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
	keep := SessionKey(r)
	if err == nil {
		err = a.Store.ChangePassword(acc.ID, keep, acc.PasswordHash, hash, a.Now())
	}
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "the password or session changed meanwhile"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot change the password"})
		return
	}
	a.revoke(func(id int64, session string) bool { return id == acc.ID && session != keep })
	a.endStreamKey(acc)
	a.endPasswordLogins(acc)
	if acc.Temporary && a.OnPermanent != nil {
		a.OnPermanent(acc.Name)
	}
	w.WriteHeader(http.StatusNoContent)
}

// startSession sets the cookie; the token for clients that asked for it
// (the desktop app, header X-Kallisti-Session: token), else "".
func (a *Service) startSession(w http.ResponseWriter, r *http.Request, acc store.Account) (string, bool) {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	if err := a.Store.CreateSession(tokenHash(token), acc.ID, a.Now().Add(sessionLifetime)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot log in"})
		return "", false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   a.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
	if r.Header.Get("X-Kallisti-Session") != "token" {
		token = ""
	}
	return token, true
}

func (a *Service) secure(r *http.Request) bool {
	return r.TLS != nil || (a.TrustForwardedFor && r.Header.Get("X-Forwarded-Proto") == "https")
}

// cors allows the web app's origin to call the API with its cookie. With
// ALLOWED_ORIGINS=* (development) only localhost origins get credentials:
// a production server set to * by mistake is not open to every site.
func (a *Service) cors(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Origin")
	origin := r.Header.Get("Origin")
	if origin == "" || !config.OriginAllowed(a.AllowedOrigins, origin) {
		return
	}
	if !slices.Contains(a.AllowedOrigins, origin) && !isLocalhost(origin) && !config.IsAppOrigin(origin) {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Kallisti-Session")
	// The desktop app sends its token, never cookies.
	if !config.IsAppOrigin(origin) {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
}

func (a *Service) allow(r *http.Request) bool {
	return a.logins.allow(clientip.Of(r, a.TrustForwardedFor), rate.Limit(perMinute)/60, burst)
}

func isLocalhost(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
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
