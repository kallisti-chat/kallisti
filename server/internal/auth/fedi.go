package auth

import (
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"kallisti.chat/server/internal/fedi"
	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/password"
	"kallisti.chat/server/internal/store"
)

// Fediverse logins: a remote account (Mastodon OAuth or Misskey MiAuth) logs in to the
// account it is linked to, makes a new account, or gets linked to the
// logged-in account. The browser goes to the instance and comes back to
// /api/fedi/callback; the flow is bound to that browser by a cookie that
// holds its state (checked against the state in the callback), and to the
// session that started a link. Remote accounts are keyed by the instance's
// host and their id there.

const (
	stateCookie   = "kallisti_fedi_state"
	signupCookie  = "kallisti_fedi_signup"
	handoffCookie = "kallisti_fedi_handoff"
	// A handoff's token waits this long for the popup to take it; one with
	// a question (see fediHandoff), for its answer.
	handoffLifetime    = 2 * time.Minute
	handoffAskLifetime = 5 * time.Minute
	fediPath           = "/api/fedi"
	flowLifetime       = 10 * time.Minute
	// A remote account without a Kallisti account: time to choose a name.
	signupLifetime = 15 * time.Minute
	// An app without a login yet is registered again after this: instances
	// may delete apps nobody used (Mastodon after a day, Pleroma after 15
	// minutes): this leaves a user 10 minutes on the instance's page.
	unusedAppLifetime = 5 * time.Minute
	// A fresh login with the linked fediverse account allows the first
	// password of an account without one, this long (see changePassword).
	reauthLifetime = 5 * time.Minute
	// Flows and sign-ups kept at most (bounded memory); more: refused.
	maxFediPending = 10_000
	// The download of a remote account's avatar, after the answer.
	avatarTimeout = 5 * time.Second
)

// FediLogin turns fediverse logins on (see Service.Fedi).
type FediLogin struct {
	Client *fedi.Client
	// Public: the server's public address, where the web app is and the
	// instance sends the browser back to (never an address from a request).
	Public string
	// AppName: the app's name on instances (users see it there).
	AppName string
	// Dev: instances may be local and plain HTTP (FEDI_INSECURE_DEV_INSTANCES;
	// the Client must allow it too).
	Dev bool
	// AllowsHome: accounts from this home server (an instance's host) may
	// log in (an admin's lists, see package homes); nil: all.
	AllowsHome func(host string) bool

	flows    pending[fediFlow]    // by state
	signups  pending[fediSignup]  // by the signup cookie
	handoffs pending[fediHandoff] // by the handoff cookie
	// Serializes handoff creation/redemption with identity and login
	// revocation, including their store changes. Never held by hooks.
	handoffMu sync.Mutex
	// Fresh logins (reauth flows) by session key: the account's id.
	proofMu sync.Mutex
	proofs  map[string]pendingEntry[int64]
	avatars sync.WaitGroup // downloads (tests wait for them)
}

// prove records a fresh login of the account in this session; false: full.
func (f *FediLogin) prove(session string, account int64, now time.Time) bool {
	f.proofMu.Lock()
	defer f.proofMu.Unlock()
	if f.proofs == nil {
		f.proofs = map[string]pendingEntry[int64]{}
	}
	if len(f.proofs) >= maxFediPending {
		for k, e := range f.proofs {
			if !now.Before(e.expires) {
				delete(f.proofs, k)
			}
		}
		if len(f.proofs) >= maxFediPending {
			return false
		}
	}
	f.proofs[session] = pendingEntry[int64]{account, now.Add(reauthLifetime)}
	return true
}

// takeProof uses up the session's fresh login of the account.
func (f *FediLogin) takeProof(session string, account int64, now time.Time) bool {
	f.proofMu.Lock()
	defer f.proofMu.Unlock()
	e, ok := f.proofs[session]
	delete(f.proofs, session)
	return ok && e.v == account && now.Before(e.expires)
}

type fediFlow struct {
	inst     fedi.Instance
	app      fedi.App
	verifier string
	scope    string
	miauth   string // native Misskey session, created here, never from the callback
	// A link (or a reauth: a fresh login with the linked account): the
	// account and its session (store key) that started it.
	link    int64
	session string
	reauth  bool
	// A handoff (see fediHandoff): another server's page adds this one;
	// to: that page's origin.
	handoff bool
	to      string
}

type fediSignup struct {
	id      store.FediIdentity
	name    string // the suggestion
	avatar  string // its URL ("": none)
	handoff string // a handoff's origin (see fediHandoff); "": none
	ask     bool   // a handoff's question (see fediHandoff)
}

// fediHandoff: the page of the account's home server adds this server (in
// a popup, see the web app): no cookie here, a session token instead, once
// (POST /api/fedi/handoff), for one origin only (the popup posts it
// there). Nobody can log this browser in here by opening the popup.
//   - Through the home server (the login's instance): for its origin, at
//     once (it vouched for the account, and could act as it anyway).
//   - Through the account's fediverse server, for its home's page (an
//     account named after its fediverse home: its home server cannot log
//     it in elsewhere): only after a yes on this server's page, for the
//     question shown (any site can open the popup, and the instance may
//     not ask the user again).
type fediHandoff struct {
	account  store.Account
	identity store.FediIdentity // the identity that authenticated this login
	origin   string
	ask      string // the question's key ("": none): a yes counts for it only
}

// pending keeps short-lived entries by random keys, in bounded memory.
type pending[T any] struct {
	mu sync.Mutex
	m  map[string]pendingEntry[T]
}

type pendingEntry[T any] struct {
	v       T
	expires time.Time
}

// put stores v until expires; its key, "" when full.
func (p *pending[T]) put(v T, now, expires time.Time) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		p.m = map[string]pendingEntry[T]{}
	}
	if len(p.m) >= maxFediPending {
		for k, e := range p.m {
			if !now.Before(e.expires) {
				delete(p.m, k)
			}
		}
		if len(p.m) >= maxFediPending {
			return ""
		}
	}
	key := randomHex(32)
	p.m[key] = pendingEntry[T]{v, expires}
	return key
}

// get finds an entry that has not expired; take removes it too.
func (p *pending[T]) get(key string, now time.Time, take bool) (T, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.m[key]
	if take {
		delete(p.m, key)
	}
	return e.v, ok && now.Before(e.expires)
}

func (f *FediLogin) redirectURI() string { return f.Public + fediPath + "/callback" }

func (f *FediLogin) allowsHome(host string) bool { return f.AllowsHome == nil || f.AllowsHome(host) }

// webOrigin: s is a page's origin, exactly (HTTPS; dev: HTTP too).
func (f *FediLogin) webOrigin(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || f.Dev && u.Scheme == "http") && u.Host != "" && u.Scheme+"://"+u.Host == s
}

// allowsIdentity: the lists allow a remote account: its instance and its
// handle's domain (an account may be served elsewhere).
func (f *FediLogin) allowsIdentity(id store.FediIdentity) bool {
	return f.allowsHome(id.Host) && f.allowsHome(id.Handle[strings.LastIndex(id.Handle, "@")+1:])
}

// EndBlockedHomes ends the logins (sessions and what lives on of them) of
// the accounts that log in only with a home server the admins' lists
// block now; after a change of the lists.
func (a *Service) EndBlockedHomes() {
	if a.Fedi == nil {
		return
	}
	accounts, err := a.Store.FediOnlyAccounts()
	if err != nil {
		slog.Error("listing fediverse-only accounts", "err", err)
		return
	}
	for _, f := range accounts {
		if a.Fedi.allowsIdentity(f.Identity) {
			continue
		}
		acc, err := a.Store.AccountByName(f.Name)
		if err == nil {
			err = a.endLogins(acc, "", func() error { return a.Store.DeleteSessionsExcept(acc.ID, "") })
		}
		if err != nil {
			slog.Error("ending a blocked home's logins", "account", f.Name, "err", err)
			continue
		}
	}
}

// home is the web app with a result for it ("fedi" and "reason").
func (f *FediLogin) home(result, reason string) string {
	if result == "" {
		return f.Public + "/"
	}
	q := url.Values{"fedi": {result}}
	if reason != "" {
		q.Set("reason", reason)
	}
	return f.Public + "/?" + q.Encode()
}

func (a *Service) setFediCookie(w http.ResponseWriter, r *http.Request, name, value string, life time.Duration) {
	// Before __Host- names they lived at /api/fedi: over plain HTTP the
	// browser would send such a stale one first.
	for _, old := range []string{stateCookie, signupCookie} {
		http.SetCookie(w, &http.Cookie{Name: old, Value: "", Path: fediPath, MaxAge: -1, HttpOnly: true})
	}
	if value == "" {
		a.clearCookie(w, r, name)
		return
	}
	maxAge := int(life.Seconds())
	// Path "/": __Host- cookies need it (see cookieName).
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(r, name), Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: a.secure(r), SameSite: http.SameSiteLaxMode})
}

// serveFedi answers /api/fedi and below (POSTs are checked as JSON and
// same-site in ServeHTTP); false: not a fediverse route.
func (a *Service) serveFedi(w http.ResponseWriter, r *http.Request) bool {
	if a.Fedi == nil {
		return false
	}
	get, post := r.Method == http.MethodGet, r.Method == http.MethodPost
	switch p := r.URL.Path; {
	case p == fediPath && get:
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
	case p == fediPath+"/start" && post:
		a.fediStart(w, r)
	case p == fediPath+"/callback" && get:
		a.fediCallback(w, r)
	case p == fediPath+"/signup" && get:
		if s, ok := a.Fedi.signups.get(a.cookie(r, signupCookie), a.Now(), false); ok {
			writeJSON(w, http.StatusOK, map[string]string{"name": s.name, "handle": s.id.Handle, "host": s.id.Host, "remote": remoteName(s.id)})
		} else {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "the fediverse login expired, please try again"})
		}
	case p == fediPath+"/signup" && post:
		if a.ownSite(w, r, "create an account on the server's own site") {
			a.fediSignup(w, r)
		}
	case p == fediPath+"/handoff" && post:
		a.takeHandoff(w, r)
	case p == fediPath+"/unlink" && post:
		a.fediUnlink(w, r)
	case p == fediPath+"/public" && post:
		a.fediPublic(w, r)
	case p == fediPath+"/handle" && get:
		h, err := a.Store.PublicFediHandle(r.URL.Query().Get("name"))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "please try again later"})
			return true
		}
		writeJSON(w, http.StatusOK, map[string]string{"handle": h})
	default:
		return false
	}
	return true
}

// fediStart discovers the instance's login method and answers with its
// authorize URL. A link needs the account's password, if it has one:
// else a stolen session could add a lasting way in.
func (a *Service) fediStart(w http.ResponseWriter, r *http.Request) {
	if !a.allow(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var c struct {
		Instance string `json:"instance"`
		Link     bool   `json:"link"`
		Password string `json:"password"`
		// "reauth": a fresh login with the linked account (see prove).
		Purpose string `json:"purpose"`
		Handoff bool   `json:"handoff"`
		// A handoff's page (an origin; "": the instance's).
		To string `json:"to"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if c.Handoff && (c.Link || c.Purpose != "") || !c.Handoff && c.To != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if c.To != "" && !a.Fedi.webOrigin(c.To) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	var inst fedi.Instance
	var err error
	flow := fediFlow{handoff: c.Handoff}
	if c.Purpose == "reauth" {
		acc, ok := a.AccountFor(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
			return
		}
		id, lookupErr := a.Store.FediIdentityOf(acc.ID)
		if lookupErr != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "log in with the fediverse account linked to this account"})
			return
		}
		inst, err = a.Fedi.reauthInstance(id.Host, c.Instance)
		flow.link, flow.session, flow.reauth = acc.ID, a.SessionKey(r), true
	} else if c.Link {
		acc, ok := a.AccountFor(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
			return
		}
		if acc.PasswordHash != "" {
			release := password.Limit()
			ok = password.Check(acc.PasswordHash, c.Password)
			release()
		}
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "the password is wrong"})
			return
		}
		flow.link, flow.session = acc.ID, a.SessionKey(r)
	}
	if !flow.reauth {
		inst, err = a.Fedi.Client.ResolveInstance(r.Context(), c.Instance, a.Fedi.Dev)
	}
	if errors.Is(err, fedi.ErrInstance) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enter your fediverse server or @user@domain"})
		return
	}
	if err != nil {
		slog.Info("fediverse discovery", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "cannot discover this account's server; try its server address instead"})
		return
	}
	flow.inst, flow.to = inst, cmp.Or(c.To, inst.Base)
	if !a.Fedi.allowsHome(inst.Host) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "this server does not take logins from " + inst.Host})
		return
	}
	if flow.app, err = a.fediApp(r.Context(), inst); err == nil {
		flow.scope, flow.verifier = fedi.Scope(flow.app.Scopes, false), fedi.NewVerifier()
	} else {
		// Keep working OAuth providers (including compatible Misskey forks)
		// independent of optional native discovery.
		misskey, discoveryErr := a.Fedi.Client.IsMisskey(r.Context(), inst)
		if discoveryErr != nil || !misskey {
			slog.Info("fediverse app registration", "instance", inst.Host, "err", err, "discovery_err", discoveryErr)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "cannot reach " + inst.Host + ", or it does not offer logins for apps"})
			return
		}
		flow.miauth = fedi.NewMiAuthSession()
	}
	authorize, ok := a.beginFlow(w, r, flow)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "too many logins right now, try again later"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": authorize})
}

// reauthInstance uses the stored API host, never the display handle's domain.
func (f *FediLogin) reauthInstance(host, input string) (fedi.Instance, error) {
	if f.Dev {
		// Preserve plain HTTP for local development instances only.
		if typed, err := fedi.ParseInstance(input, true); err == nil && typed.Host == host {
			return typed, nil
		}
	}
	return fedi.ParseInstance(host, f.Dev)
}

// displayHandle prefers a confirmed account-domain address. Authentication already
// established the account, so optional discovery failure keeps logins working.
// confirmed: false when discovery failed (the instance's own address).
func (f *FediLogin) displayHandle(ctx context.Context, inst fedi.Instance, remote fedi.Account) (handle string, confirmed bool) {
	handle, err := f.Client.DiscoverHandle(ctx, inst, remote.Username, f.Dev)
	if err != nil {
		slog.Debug("fediverse display handle", "instance", inst.Host, "err", err)
		return remote.Handle(inst), false
	}
	return handle, true
}

// fediApp is our app on the instance, registered on first use (and again
// while none of its logins went through, see unusedAppLifetime; for each
// login where one login uses it up, see fedi.SingleUse).
func (a *Service) fediApp(ctx context.Context, inst fedi.Instance) (fedi.App, error) {
	redirect := a.Fedi.redirectURI()
	app, err := a.Store.FediApp(inst.Host, redirect)
	if err == nil && !fedi.SingleUse(inst, app.ClientID) && (app.Used || a.Now().Sub(app.CreatedAt) < unusedAppLifetime) {
		return fedi.App{ClientID: app.ClientID, ClientSecret: app.ClientSecret, Scopes: app.Scopes}, nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fedi.App{}, err
	}
	registered, err := a.Fedi.Client.RegisterApp(ctx, inst, a.Fedi.AppName, redirect, a.Fedi.Public)
	if err != nil {
		return fedi.App{}, err
	}
	err = a.Store.SaveFediApp(store.FediApp{Host: inst.Host, RedirectURI: redirect, Scopes: registered.Scopes,
		ClientID: registered.ClientID, ClientSecret: registered.ClientSecret}, a.Now())
	return registered, err
}

// beginFlow keeps the flow, sets the state cookie, and gives the URL that
// sends the browser to the instance.
func (a *Service) beginFlow(w http.ResponseWriter, r *http.Request, flow fediFlow) (string, bool) {
	now := a.Now()
	state := a.Fedi.flows.put(flow, now, now.Add(flowLifetime))
	if state == "" {
		return "", false
	}
	a.setFediCookie(w, r, stateCookie, state, flowLifetime)
	if flow.miauth != "" {
		// MiAuth returns "session", not OAuth's "state": carry our browser
		// binding in the callback URL itself.
		callback := withQuery(a.Fedi.redirectURI(), url.Values{"state": {state}})
		return fedi.MiAuthURL(flow.inst, flow.miauth, a.Fedi.AppName, callback), true
	}
	return fedi.AuthorizeURL(flow.inst, flow.app.ClientID, a.Fedi.redirectURI(), flow.scope, state, fedi.Challenge(flow.verifier)), true
}

// fediCallback: the instance sends the browser back here. Always answers
// with a redirect to the web app (a fixed address), with the result.
func (a *Service) fediCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer") // the code is in the URL
	back := func(result, reason string) { http.Redirect(w, r, a.Fedi.home(result, reason), http.StatusFound) }
	// It counts if the state in the callback is the cookie's; only then
	// the flow ends (once), so a link from anywhere cannot cancel it.
	state := a.cookie(r, stateCookie)
	q := r.URL.Query()
	if state == "" || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
		back("error", "expired")
		return
	}
	flow, ok := a.Fedi.flows.get(state, a.Now(), true)
	a.setFediCookie(w, r, stateCookie, "", 0)
	if !ok {
		back("error", "expired")
		return
	}
	if flow.miauth != "" && subtle.ConstantTimeCompare([]byte(q.Get("session")), []byte(flow.miauth)) != 1 {
		back("error", "expired")
		return
	}
	if e := q.Get("error"); e != "" || flow.miauth == "" && q.Get("code") == "" {
		// An instance that does not allow "profile" for this app: once more
		// with read:accounts.
		if flow.miauth == "" && e == "invalid_scope" && flow.scope != fedi.Scope("", true) {
			flow.scope, flow.verifier = fedi.Scope("", true), fedi.NewVerifier()
			if next, ok := a.beginFlow(w, r, flow); ok {
				http.Redirect(w, r, next, http.StatusFound)
				return
			}
		}
		back("error", "denied")
		return
	}
	// Blocked since the start.
	if !a.Fedi.allowsHome(flow.inst.Host) {
		back("error", "blocked")
		return
	}
	// A link: still the same session in this browser.
	linkTo, ok := a.AccountFor(r)
	if flow.link != 0 && (!ok || linkTo.ID != flow.link || a.SessionKey(r) != flow.session) {
		back("error", "expired")
		return
	}
	remote, err := a.remoteAccount(r.Context(), flow, q.Get("code"))
	if err != nil {
		slog.Info("fediverse login", "instance", flow.inst.Host, "err", err)
		back("error", "unreachable")
		return
	}
	handle, confirmed := a.Fedi.displayHandle(r.Context(), flow.inst, remote)
	id := store.FediIdentity{Host: flow.inst.Host, RemoteID: remote.ID, Handle: handle}
	// A known account: its confirmed handle (from an earlier login) counts
	// too, and a failed lookup does not replace it.
	acc, known, knownErr := a.Store.AccountByFedi(id.Host, id.RemoteID)
	if knownErr == nil && !confirmed {
		id.Handle = known.Handle
	}
	if !a.Fedi.allowsIdentity(id) {
		back("error", "blocked")
		return
	}
	// A newly confirmed handle is kept (on every path, a reauth too).
	if knownErr == nil && confirmed && known.Handle != id.Handle {
		a.Store.UpdateFediHandle(id.Host, id.RemoteID, id.Handle)
	}
	if flow.reauth {
		back(a.fediReauth(linkTo, flow.session, id))
		return
	}
	if flow.link != 0 {
		result, reason := a.fediLink(linkTo, flow.session, id)
		if result == "linked" {
			a.fediAvatar(linkTo, remote.Avatar, true)
		}
		back(result, reason)
		return
	}
	switch err := knownErr; {
	case err == nil:
		if flow.handoff {
			if a.handOff(w, r, acc, id, flow.to, flow.to != flow.inst.Base) {
				back("connect", "")
			} else {
				back("error", "failed")
			}
			return
		}
		if _, ok := a.startSession(w, r, acc); !ok {
			return
		}
		back("", "")
	case errors.Is(err, store.ErrNotFound):
		now := a.Now()
		s := fediSignup{id: id, name: fedi.SuggestName(remote.Username), avatar: remote.Avatar}
		if flow.handoff {
			s.handoff, s.ask = flow.to, flow.to != flow.inst.Base
		}
		token := a.Fedi.signups.put(s, now, now.Add(signupLifetime))
		if token == "" {
			back("error", "failed")
			return
		}
		a.setFediCookie(w, r, signupCookie, token, signupLifetime)
		back("register", "")
	default:
		slog.Error("fediverse login", "err", err)
		back("error", "failed")
	}
}

// remoteAccount completes the authorization and reads the account. The token
// is not kept (nor revoked: instances may delete apps without one).
func (a *Service) remoteAccount(ctx context.Context, flow fediFlow, code string) (fedi.Account, error) {
	if flow.miauth != "" {
		return a.Fedi.Client.MiAuthAccount(ctx, flow.inst, flow.miauth)
	}
	token, err := a.Fedi.Client.Token(ctx, flow.inst, flow.app, a.Fedi.redirectURI(), code, flow.verifier)
	if err != nil {
		return fedi.Account{}, err
	}
	remote, err := a.Fedi.Client.VerifyCredentials(ctx, flow.inst, token)
	var status *fedi.StatusError
	if errors.As(err, &status) && (status.Code == http.StatusUnauthorized || status.Code == http.StatusForbidden) &&
		flow.scope == fedi.ScopeProfile {
		// A "profile" token that cannot read the account (Pleroma takes
		// the scope, see fedi.KnowsProfile): register again next time.
		if err := a.Store.ForgetFediApp(flow.inst.Host); err != nil {
			slog.Error("fediverse app", "err", err)
		}
		return fedi.Account{}, err
	}
	if err != nil {
		return fedi.Account{}, err
	}
	// Used only once the account was read: a kept app must work.
	if err := a.Store.MarkFediAppUsed(flow.inst.Host); err != nil {
		slog.Error("fediverse app", "err", err)
	}
	return remote, nil
}

// fediLink links the remote account to the account; the result for home.
func (a *Service) fediLink(acc store.Account, session string, id store.FediIdentity) (string, string) {
	err := a.Store.LinkFedi(acc.ID, session, id, a.Now())
	if errors.Is(err, store.ErrFediTaken) {
		return "error", "taken"
	}
	if errors.Is(err, store.ErrSessionGone) {
		return "error", "expired"
	}
	if err != nil {
		slog.Error("link fediverse account", "err", err)
		return "error", "failed"
	}
	if acc.Temporary && a.OnPermanent != nil {
		a.OnPermanent(acc.Name)
	}
	return "linked", ""
}

// fediReauth records a fresh login with the account's linked remote
// account, for this session; the result for home.
func (a *Service) fediReauth(acc store.Account, session string, id store.FediIdentity) (string, string) {
	linked, err := a.Store.FediIdentityOf(acc.ID)
	if err != nil || linked.Host != id.Host || linked.RemoteID != id.RemoteID {
		return "error", "other"
	}
	if !a.Fedi.prove(session, acc.ID, a.Now()) {
		return "error", "failed"
	}
	return "reauth", ""
}

// fediSignup makes the account for a remote account without one, with the
// chosen name: the same checks as other new accounts (name, browser check).
func (a *Service) fediSignup(w http.ResponseWriter, r *http.Request) {
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
	key := a.cookie(r, signupCookie)
	s, ok := a.Fedi.signups.get(key, a.Now(), false) // kept for another name after a clash
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "the fediverse login expired, please try again"})
		return
	}
	// Blocked since the callback (a sign-up may wait 15 minutes).
	if !a.Fedi.allowsIdentity(s.id) {
		a.Fedi.signups.get(key, a.Now(), true)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "this server does not take logins from " + s.id.Host})
		return
	}
	// No name: its remote one (name@home); else one of here (the name step,
	// when the remote one is taken).
	var name string
	if strings.TrimSpace(c.Name) == "" {
		name, ok = a.checkRemoteName(w, remoteName(s.id))
	} else {
		name, ok = a.checkName(w, c.Name)
	}
	if !ok || !a.proven(w, c) {
		return
	}
	acc, token, ok := a.newAccount(w, r, store.Account{Name: name}, s.handoff == "", func(acc store.Account) (store.Account, error) {
		return a.Store.CreateFediAccount(acc, s.id, a.Now(), c.Invite)
	})
	if !ok {
		return
	}
	a.Fedi.signups.get(key, a.Now(), true)
	a.setFediCookie(w, r, signupCookie, "", 0)
	if s.handoff != "" {
		if !a.handOff(w, r, acc, s.id, s.handoff, s.ask) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "please try again"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"name": acc.Name, "handoff": true})
	} else {
		writeJSON(w, http.StatusCreated, withToken(map[string]any{"name": acc.Name}, token))
	}
	a.fediAvatar(acc, s.avatar, false)
}

// fediAvatar gives the account the remote account's avatar (from its URL,
// "": none), in the background: the answer does not wait for the
// instance. Failures leave the account as it is.
func (a *Service) fediAvatar(acc store.Account, avatarURL string, onlyIfNone bool) {
	if avatarURL == "" || a.SetAvatar == nil {
		return
	}
	a.Fedi.avatars.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), avatarTimeout)
		defer cancel()
		avatar, err := a.Fedi.Client.Avatar(ctx, avatarURL)
		if err != nil {
			slog.Info("fediverse avatar", "account", acc.Name, "err", err)
			return
		}
		a.SetAvatar(acc, avatar, onlyIfNone)
	})
}

func (a *Service) fediUnlink(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.AccountFor(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}
	// Named after its home (name@home): the name needs the link behind it.
	if _, host := names.Split(acc.Name); host != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "an account named after its home server stays linked to it"})
		return
	}
	err := a.revokeHandoffs(acc.ID, func() error {
		err := a.Store.UnlinkFedi(acc.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	})
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrLastLogin):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "set a password first: this is your only way to log in"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot unlink, please try again"})
	}
}

// fediPublic shows or hides the linked handle on the profile.
func (a *Service) fediPublic(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.AccountFor(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}
	var c struct {
		On bool `json:"on"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	switch err := a.Store.SetFediPublic(acc.ID, c.On); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no fediverse account is linked"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot save, please try again"})
	}
}

// fediMe: the linked remote account for /api/me (nil: none).
func (a *Service) fediMe(acc store.Account) map[string]any {
	id, err := a.Store.FediIdentityOf(acc.ID)
	if err != nil {
		return nil
	}
	// host: the server it logs in with (see fediHandoff).
	return map[string]any{"handle": id.Handle, "public": id.Public, "host": id.Host}
}

// revokeHandoffs runs a revocation and drops the account's pending handoffs
// only on success. Holding the lock across the store change prevents a
// redemption from making a session just after the old sessions were deleted.
func (a *Service) revokeHandoffs(account int64, revoke func() error) error {
	if a.Fedi == nil {
		return revoke()
	}
	f := a.Fedi
	f.handoffMu.Lock()
	defer f.handoffMu.Unlock()
	if err := revoke(); err != nil {
		return err
	}
	f.handoffs.mu.Lock()
	defer f.handoffs.mu.Unlock()
	for key, e := range f.handoffs.m {
		if e.v.account.ID == account {
			delete(f.handoffs.m, key)
		}
	}
	return nil
}

// handOff keeps the account for the popup (see fediHandoff): its session
// starts when the popup takes it.
func (a *Service) handOff(w http.ResponseWriter, r *http.Request, acc store.Account, id store.FediIdentity, origin string, ask bool) bool {
	a.Fedi.handoffMu.Lock()
	defer a.Fedi.handoffMu.Unlock()
	// The callback's lookup may predate an unlink or password change.
	current, _, err := a.Store.AccountByFedi(id.Host, id.RemoteID)
	if err != nil || current.ID != acc.ID || current.PasswordHash != acc.PasswordHash {
		return false
	}
	h, now, life := fediHandoff{account: current, identity: id, origin: origin}, a.Now(), handoffLifetime
	if ask {
		h.ask, life = randomHex(16), handoffAskLifetime
	}
	key := a.Fedi.handoffs.put(h, now, now.Add(life))
	if key == "" {
		return false
	}
	a.setFediCookie(w, r, handoffCookie, key, life)
	return true
}

// takeHandoff gives the popup a new session's token, once (from this
// site's own page). One with a question: first the question (its key,
// the name and the origin; nothing taken), then the token for {"allow":
// key}.
func (a *Service) takeHandoff(w http.ResponseWriter, r *http.Request) {
	if !a.ownSite(w, r, "only this server's own page takes it") {
		return
	}
	var c struct {
		Allow string `json:"allow"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	a.Fedi.handoffMu.Lock()
	defer a.Fedi.handoffMu.Unlock()
	key := a.cookie(r, handoffCookie)
	h, ok := a.Fedi.handoffs.get(key, a.Now(), false)
	var acc store.Account
	if ok {
		var id store.FediIdentity
		var err error
		acc, id, err = a.Store.AccountByFedi(h.identity.Host, h.identity.RemoteID)
		if err != nil || acc.ID != h.account.ID || acc.PasswordHash != h.account.PasswordHash {
			a.Fedi.handoffs.get(key, a.Now(), true)
			ok = false // missing identity, changed account, or failed lookup: no login
		} else if !a.Fedi.allowsIdentity(id) {
			a.Fedi.handoffs.get(key, a.Now(), true)
			a.setFediCookie(w, r, handoffCookie, "", 0)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "this server does not take accounts from your home server"})
			return
		}
	}
	switch {
	case ok && h.ask != "" && c.Allow == "":
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"ask": h.ask, "name": acc.Name, "origin": h.origin})
		return
	case ok && h.ask != "" && subtle.ConstantTimeCompare([]byte(c.Allow), []byte(h.ask)) != 1:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "another login replaced this one, please try again"})
		return
	}
	// Taken once, with expiry checked again before session creation.
	h, ok = a.Fedi.handoffs.get(key, a.Now(), true)
	a.setFediCookie(w, r, handoffCookie, "", 0)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "the login expired, please try again"})
		return
	}
	token, err := a.newSession(acc)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "please try again"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "name": acc.Name, "origin": h.origin})
}

// remoteName: a remote account's name here, as the fediverse writes it
// (its confirmed handle without the first "@").
func remoteName(id store.FediIdentity) string { return strings.TrimPrefix(id.Handle, "@") }
