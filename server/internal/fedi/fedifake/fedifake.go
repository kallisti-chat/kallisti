// Package fedifake is a small fake fediverse instance (Mastodon OAuth or
// native Misskey OAuth), for tests and local checks.
// Never run it where real users can reach it: it approves every login.
package fedifake

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// Instance serves /api/v1/apps, /oauth/authorize (a page that approves
// at once, for any user name), /oauth/token and
// /api/v1/accounts/verify_credentials, and the avatar (see SetAvatar).
// The zero value works.
type Instance struct {
	// NoProfileScope: an older server that does not know the "profile"
	// scope (Mastodon before 4.3): apps asking for it are refused.
	NoProfileScope bool
	// ProfileDenied: apps may have "profile", but authorizing it fails
	// (back to the app with error=invalid_scope).
	ProfileDenied bool
	// Version is what /api/v1/instance says ("": "4.3.0", a Mastodon
	// that knows "profile").
	Version string
	// IgnoresProfile: as Pleroma, apps and logins may ask for "profile",
	// but a token without read:accounts cannot read the account.
	IgnoresProfile bool
	// SingleUseApps: as Misskey and Sharkey, an app's client id is base64
	// of an auth session URL (<base>/auth/<id>), and one login uses it up.
	SingleUseApps bool
	// NativeMisskey offers public-client OAuth, without Mastodon-compatible APIs.
	NativeMisskey bool
	// NoIssuer simulates native servers without RFC 9207 response support.
	NoIssuer bool

	mu       sync.Mutex
	apps     map[string]app   // by client id
	codes    map[string]grant // by code
	tokens   map[string]string
	limited  map[string]bool   // tokens that cannot read the account
	users    map[string]string // user name: id
	requests int
	avatar   []byte
}

// SetAvatar gives all users this avatar (nil: none), at /media/avatar and
// /media/avatar_static.
func (f *Instance) SetAvatar(data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.avatar = data
}

type app struct{ secret, redirect, scopes string }

type grant struct{ client, redirect, challenge, user, scope string }

func random() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Requests counts the requests the instance got.
func (f *Instance) Requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *Instance) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests++
	if f.apps == nil {
		f.apps, f.codes, f.tokens, f.users = map[string]app{}, map[string]grant{}, map[string]string{}, map[string]string{}
		f.limited = map[string]bool{}
	}
	f.mu.Unlock()
	if f.NativeMisskey && strings.HasPrefix(r.URL.Path, "/api/v1/") {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	switch {
	case f.NativeMisskey && r.URL.Path == "/.well-known/oauth-authorization-server" && r.Method == http.MethodGet:
		base := requestOrigin(r)
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer": base, "authorization_endpoint": base + "/oauth/authorize", "token_endpoint": base + "/oauth/token",
			"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"},
			"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"},
			"scopes_supported": []string{"read:account"}, "authorization_response_iss_parameter_supported": !f.NoIssuer,
		})
	case f.NativeMisskey && r.URL.Path == "/api/i" && r.Method == http.MethodPost:
		f.verifyMisskey(w, r)
	case f.NativeMisskey && r.URL.Path == "/api/i/revoke-token" && r.Method == http.MethodPost:
		f.revokeMisskey(w, r)
	case r.URL.Path == "/api/v1/instance" && r.Method == http.MethodGet:
		version := f.Version
		if version == "" {
			version = "4.3.0"
		}
		writeJSON(w, http.StatusOK, map[string]string{"uri": r.Host, "version": version})
	case r.URL.Path == "/api/v1/apps" && r.Method == http.MethodPost:
		f.register(w, r)
	case r.URL.Path == "/oauth/authorize" && r.Method == http.MethodGet:
		f.authorizePage(w, r)
	case r.URL.Path == "/oauth/authorize" && r.Method == http.MethodPost:
		callback, err := f.approve(r.Form, r.PostForm.Get("user"), requestOrigin(r))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, callback, http.StatusFound)
	case r.URL.Path == "/oauth/token" && r.Method == http.MethodPost:
		f.token(w, r)
	case r.URL.Path == "/api/v1/accounts/verify_credentials" && r.Method == http.MethodGet:
		f.verify(w, r)
	case (r.URL.Path == "/media/avatar" || r.URL.Path == "/media/avatar_static") && r.Method == http.MethodGet:
		f.mu.Lock()
		avatar := f.avatar
		f.mu.Unlock()
		if avatar == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(avatar)
	default:
		http.NotFound(w, r)
	}
}

func requestOrigin(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func (f *Instance) register(w http.ResponseWriter, r *http.Request) {
	scopes := strings.Fields(r.PostForm.Get("scopes"))
	if len(scopes) == 0 || f.NoProfileScope && slices.Contains(scopes, "profile") {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Validation failed: Scopes are invalid"})
		return
	}
	id := random()
	if f.SingleUseApps {
		id = base64.StdEncoding.EncodeToString([]byte("https://" + r.Host + "/auth/" + id))
	}
	a := app{secret: random(), redirect: r.PostForm.Get("redirect_uris"), scopes: strings.Join(scopes, " ")}
	f.mu.Lock()
	f.apps[id] = a
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"id": "1", "name": r.PostForm.Get("client_name"), "client_id": id, "client_secret": a.secret, "redirect_uri": a.redirect})
}

var page = template.Must(template.New("").Parse(`<!doctype html><title>Fake instance: authorize</title>
<h1>Fake instance</h1><p>Log in to Kallisti as:</p>
<form method="post" action="/oauth/authorize">
{{range $k, $v := .}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}
<input name="user" value="alice" aria-label="User name"><button type="submit">Authorize</button></form>`))

func (f *Instance) authorizePage(w http.ResponseWriter, r *http.Request) {
	if _, err := f.check(r.Form); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page.Execute(w, r.Form)
}

// check validates an authorization request.
func (f *Instance) check(q url.Values) (app, error) {
	f.mu.Lock()
	a, ok := f.apps[q.Get("client_id")]
	f.mu.Unlock()
	if f.NativeMisskey {
		var err error
		a, err = misskeyClient(q.Get("client_id"))
		if err != nil {
			return app{}, err
		}
		ok = true
		if q.Get("scope") != "read:account" {
			return app{}, errors.New("invalid scope")
		}
	}
	switch {
	case !ok:
		return app{}, errors.New("unknown client")
	case q.Get("redirect_uri") != a.redirect:
		return app{}, errors.New("wrong redirect_uri")
	case q.Get("response_type") != "code":
		return app{}, errors.New("wrong response_type")
	case q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "":
		return app{}, errors.New("PKCE (S256) needed")
	}
	for _, s := range strings.Fields(q.Get("scope")) {
		if !slices.Contains(strings.Fields(a.scopes), s) {
			return app{}, errors.New("invalid scope")
		}
	}
	return a, nil
}

// Approve is the user's "Authorize" on the page for an authorize URL:
// the callback URL the browser would go to next.
func (f *Instance) Approve(authorizeURL, user string) (string, error) {
	u, err := url.Parse(authorizeURL)
	if err != nil {
		return "", err
	}
	return f.approve(u.Query(), user, u.Scheme+"://"+u.Host)
}

func (f *Instance) approve(q url.Values, user, issuer string) (string, error) {
	a, err := f.check(q)
	if err != nil {
		return "", err
	}
	if user == "" {
		return "", errors.New("no user")
	}
	cb, _ := url.Parse(a.redirect)
	v := cb.Query()
	v.Set("state", q.Get("state"))
	if f.NativeMisskey && !f.NoIssuer {
		v.Set("iss", issuer)
	}
	if f.ProfileDenied && slices.Contains(strings.Fields(q.Get("scope")), "profile") {
		v.Set("error", "invalid_scope")
	} else {
		code := random()
		f.mu.Lock()
		f.codes[code] = grant{client: q.Get("client_id"), redirect: a.redirect, challenge: q.Get("code_challenge"), user: user, scope: q.Get("scope")}
		f.mu.Unlock()
		v.Set("code", code)
	}
	cb.RawQuery = v.Encode()
	return cb.String(), nil
}

func (f *Instance) token(w http.ResponseWriter, r *http.Request) {
	p := r.PostForm
	if f.NativeMisskey && strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		p = url.Values{}
		for key, value := range body {
			p.Set(key, value)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.codes[p.Get("code")]
	delete(f.codes, p.Get("code")) // once
	a := f.apps[g.client]
	sum := sha256.Sum256([]byte(p.Get("code_verifier")))
	if !ok || p.Get("grant_type") != "authorization_code" || p.Get("client_id") != g.client || p.Get("client_secret") != a.secret ||
		p.Get("redirect_uri") != g.redirect || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	if f.SingleUseApps {
		delete(f.apps, g.client)
	}
	token := random()
	f.tokens[token] = g.user
	f.limited[token] = f.IgnoresProfile && !slices.Contains(strings.Fields(g.scope), "read:accounts")
	writeJSON(w, http.StatusOK, map[string]string{"access_token": token, "token_type": "Bearer"})
}

func (f *Instance) verify(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	user, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "The access token is invalid"})
		return
	}
	if f.limited[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Insufficient permissions: read:accounts."})
		return
	}
	id, ok := f.users[user]
	if !ok {
		id = strconv.Itoa(100 + len(f.users))
		f.users[user] = id
	}
	acc := map[string]string{"id": id, "username": user, "acct": user, "display_name": user}
	if f.avatar != nil {
		base := "https://" + r.Host
		if r.TLS == nil {
			base = "http://" + r.Host
		}
		acc["avatar"], acc["avatar_static"] = base+"/media/avatar", base+"/media/avatar_static"
	}
	writeJSON(w, http.StatusOK, acc)
}

// misskeyClient fetches the real public HTML metadata rather than trusting
// redirect_uri from the authorization request.
func misskeyClient(clientID string) (app, error) {
	u, err := url.Parse(clientID)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return app{}, errors.New("invalid client_id URL")
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(clientID)
	if err != nil {
		return app{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return app{}, errors.New("invalid client metadata response")
	}
	z := html.NewTokenizer(io.LimitReader(resp.Body, 64<<10))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return app{}, errors.New("client metadata has no redirect_uri")
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			if token.Data != "link" && token.Data != "a" {
				continue
			}
			var rel, href string
			for _, attr := range token.Attr {
				switch attr.Key {
				case "rel":
					rel = attr.Val
				case "href":
					href = attr.Val
				}
			}
			if slices.Contains(strings.Fields(rel), "redirect_uri") && href != "" {
				return app{redirect: href, scopes: "read:account"}, nil
			}
		}
	}
}

func (f *Instance) verifyMisskey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"i"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	user, ok := f.tokens[body.Token]
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}
	writeJSON(w, http.StatusOK, f.misskeyUser(r, user))
}

func (f *Instance) revokeMisskey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Auth  string `json:"i"`
		Token string `json:"token"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Auth == "" || body.Auth != body.Token {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tokens[body.Auth]; !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	delete(f.tokens, body.Token)
	w.WriteHeader(http.StatusNoContent)
}

// misskeyUser is called with mu held and shares IDs with the OAuth API.
func (f *Instance) misskeyUser(r *http.Request, user string) map[string]any {
	id, ok := f.users[user]
	if !ok {
		id = strconv.Itoa(100 + len(f.users))
		f.users[user] = id
	}
	account := map[string]any{"id": id, "username": user, "host": nil}
	if f.avatar != nil {
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		account["avatarUrl"] = scheme + "://" + r.Host + "/media/avatar"
	}
	return account
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
