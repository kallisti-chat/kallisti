package auth

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"kallisti.chat/server/internal/fedi"
	"kallisti.chat/server/internal/fedi/fedifake"
	"kallisti.chat/server/internal/store"
)

// home is the web app's address (the server's public one) in fediSetup.
var home string

// fediSetup: a server with fediverse logins and a fake instance (on
// loopback, which only tests allow).
func fediSetup(t *testing.T, f *fedifake.Instance) (*Service, *httptest.Server, string) {
	t.Helper()
	a, srv := setup(t)
	inst := httptest.NewTLSServer(f)
	t.Cleanup(inst.Close)
	client := fedi.NewClient(func(netip.AddrPort) bool { return true }, inst.Client().Transport.(*http.Transport).TLSClientConfig, false)
	a.Fedi = &FediLogin{Client: client, Public: srv.URL, AppName: "Kallisti", Dev: true}
	home = srv.URL + "/"
	return a, srv, inst.URL
}

// browser keeps cookies and does not follow redirects.
type browser struct {
	t   *testing.T
	srv *httptest.Server
	c   *http.Client
}

func newBrowser(t *testing.T, srv *httptest.Server) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t, srv, &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (b *browser) do(method, path string, body any) (*http.Response, map[string]any) {
	b.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	} else {
		rd = bytes.NewReader(nil)
	}
	u := path
	if !strings.HasPrefix(path, "http") {
		u = b.srv.URL + path
	}
	req, _ := http.NewRequest(method, u, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Back from the instance: a navigation from another site.
	if strings.Contains(u, "/api/fedi/callback") {
		req.Header.Set("Sec-Fetch-Site", "cross-site")
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	return resp, v
}

// fediFlow starts a login (or a link) on the instance and approves it as
// user; the callback's answer.
func (b *browser) fediFlow(f *fedifake.Instance, start map[string]any, user string) *http.Response {
	b.t.Helper()
	resp, v := b.do("POST", "/api/fedi/start", start)
	if resp.StatusCode != http.StatusOK {
		b.t.Fatalf("start: %d %v", resp.StatusCode, v)
	}
	callback, err := f.Approve(v["url"].(string), user)
	if err != nil {
		b.t.Fatal(err)
	}
	resp, _ = b.do("GET", callback, nil)
	return resp
}

func wantRedirect(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
		t.Fatalf("redirect: %d %q, want %q", resp.StatusCode, resp.Header.Get("Location"), want)
	}
}

func TestFediRegisterAndLogIn(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	b := newBrowser(t, srv)
	if resp, v := b.do("GET", "/api/fedi", nil); resp.StatusCode != 200 || v["enabled"] != true {
		t.Fatalf("status: %d %v", resp.StatusCode, v)
	}
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": inst}, "Alice"), home+"?fedi=register")
	resp, v := b.do("GET", "/api/fedi/signup", nil)
	if resp.StatusCode != 200 || v["name"] != "Alice" || v["handle"] != "@Alice@"+host || v["host"] != host {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
	if resp, _ := b.do("POST", "/api/fedi/signup", map[string]any{"name": "a b"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad name: %d", resp.StatusCode)
	}
	resp, v = b.do("POST", "/api/fedi/signup", map[string]any{"name": "alice"})
	if resp.StatusCode != http.StatusCreated || v["name"] != "alice" || sessionCookie(resp) == nil {
		t.Fatalf("create: %d %v", resp.StatusCode, v)
	}
	resp, v = b.do("GET", "/api/me", nil)
	if v["name"] != "alice" || v["password"] != false || v["fedi"].(map[string]any)["handle"] != "@Alice@"+host {
		t.Fatalf("me: %v", v)
	}
	// The sign-up is used up.
	if resp, _ := b.do("POST", "/api/fedi/signup", map[string]any{"name": "alice2"}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second sign-up: %d", resp.StatusCode)
	}
	// No password, no password login.
	if resp, _ := b.do("POST", "/api/login", map[string]any{"name": "alice", "password": ""}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("empty password: %d", resp.StatusCode)
	}

	// Another browser logs in with the same remote account.
	b2 := newBrowser(t, srv)
	resp = b2.fediFlow(f, map[string]any{"instance": inst}, "Alice")
	wantRedirect(t, resp, home)
	if sessionCookie(resp) == nil {
		t.Fatal("no session")
	}
	if _, v := b2.do("GET", "/api/me", nil); v["name"] != "alice" {
		t.Fatalf("me after fedi login: %v", v)
	}
}

func TestFediStateIsChecked(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	_, v := b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	callback, _ := f.Approve(v["url"].(string), "mallory")

	// Another browser (no state cookie) gets nothing from the callback: an
	// attacker cannot log a victim in to the attacker's account.
	other := newBrowser(t, srv)
	resp, _ := other.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=error&reason=expired")
	if sessionCookie(resp) != nil {
		t.Fatal("a session without the state cookie")
	}
	// A wrong or missing state in the right browser fails too, but leaves
	// the flow alone (a link from anywhere cannot cancel a login); the real
	// callback then works, once.
	cb, _ := url.Parse(callback)
	for _, state := range []string{"wrong", ""} {
		q := cb.Query()
		q.Set("state", state)
		resp, _ = b.do("GET", srv.URL+"/api/fedi/callback?"+q.Encode(), nil)
		wantRedirect(t, resp, home+"?fedi=error&reason=expired")
	}
	resp, _ = b.do("GET", callback, nil)
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "reason=expired") {
		t.Fatalf("the real callback after a wrong one: %s", loc)
	}
	resp, _ = b.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=error&reason=expired")

	// The user said no on the instance.
	_, v = b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	u, _ := url.Parse(v["url"].(string))
	resp, _ = b.do("GET", "/api/fedi/callback?error=access_denied&state="+u.Query().Get("state"), nil)
	wantRedirect(t, resp, home+"?fedi=error&reason=denied")
}

func TestFediStartRefusesBadInstances(t *testing.T) {
	a, srv := setup(t)
	a.Fedi = &FediLogin{Client: fedi.NewClient(nil, nil, false), Public: "https://app.example", AppName: "Kallisti"}
	b := newBrowser(t, srv)
	for _, inst := range []string{"", "localhost", "http://social.example", "127.0.0.1"} {
		if resp, _ := b.do("POST", "/api/fedi/start", map[string]any{"instance": inst}); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: %d", inst, resp.StatusCode)
		}
	}
	// Off: no fediverse routes.
	a.Fedi = nil
	if resp, _ := b.do("GET", "/api/fedi", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("off: %d", resp.StatusCode)
	}
}

func TestFediScopeFallback(t *testing.T) {
	f := &fedifake.Instance{ProfileDenied: true}
	_, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	_, v := b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	callback, _ := f.Approve(v["url"].(string), "carol")
	// invalid_scope: back to the instance, with read:accounts.
	resp, _ := b.do("GET", callback, nil)
	next, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound || next.Query().Get("scope") != "read:accounts" {
		t.Fatalf("fallback: %d %s", resp.StatusCode, next)
	}
	callback, err := f.Approve(next.String(), "carol")
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = b.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=register")
}

func TestFediAsksPleromaForReadAccounts(t *testing.T) {
	// Pleroma and Akkoma take an app with "profile" but give a token that
	// cannot read the account: ask such servers for read:accounts.
	f := &fedifake.Instance{Version: "2.7.2 (compatible; Pleroma 2.6.0)", IgnoresProfile: true}
	_, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	_, v := b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	authorize, _ := url.Parse(v["url"].(string))
	if got := authorize.Query().Get("scope"); got != "read:accounts" {
		t.Fatalf("scope: %q", got)
	}
	callback, err := f.Approve(authorize.String(), "lain")
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := b.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=register")
}

func TestFediForgetsAnAppWhoseProfileTokenCannotReadTheAccount(t *testing.T) {
	// An app registered with "profile" before the version check, on a
	// server that cannot read the account with it (Pleroma): the failed
	// login does not keep the app, and the next one registers again.
	f := &fedifake.Instance{IgnoresProfile: true} // says it is Mastodon 4.3
	_, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	if resp := b.fediFlow(f, map[string]any{"instance": inst}, "pwm"); !strings.Contains(resp.Header.Get("Location"), "fedi=error") {
		t.Fatalf("first login: %s", resp.Header.Get("Location"))
	}
	f.Version = "3.0.666 (compatible; Pleroma 2.10.2)"
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": inst}, "pwm"), home+"?fedi=register")
}

func TestFediLinkAndUnlink(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	b.do("POST", "/api/accounts", map[string]any{"name": "bob", "password": "longpassword"})

	// Linking needs the current password (a stolen session cannot add a
	// way in) and a login.
	if resp, _ := b.do("POST", "/api/fedi/start", map[string]any{"instance": inst, "link": true, "password": "wrong password"}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if resp, _ := newBrowser(t, srv).do("POST", "/api/fedi/start", map[string]any{"instance": inst, "link": true}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("link without login: %d", resp.StatusCode)
	}
	link := map[string]any{"instance": inst, "link": true, "password": "longpassword"}
	wantRedirect(t, b.fediFlow(f, link, "bobby"), home+"?fedi=linked")
	if _, v := b.do("GET", "/api/me", nil); v["fedi"] == nil || v["password"] != true {
		t.Fatalf("me: %v", v)
	}
	// The remote account logs in to bob now.
	b2 := newBrowser(t, srv)
	wantRedirect(t, b2.fediFlow(f, map[string]any{"instance": inst}, "bobby"), home)
	if _, v := b2.do("GET", "/api/me", nil); v["name"] != "bob" {
		t.Fatalf("login after link: %v", v)
	}

	// A remote account linked elsewhere cannot be linked again.
	c := newBrowser(t, srv)
	c.do("POST", "/api/accounts", map[string]any{"name": "carol", "password": "longpassword"})
	wantRedirect(t, c.fediFlow(f, map[string]any{"instance": inst, "link": true, "password": "longpassword"}, "bobby"), home+"?fedi=error&reason=taken")

	// A link started in one session does not finish in another one.
	_, v := c.do("POST", "/api/fedi/start", map[string]any{"instance": inst, "link": true, "password": "longpassword"})
	callback, _ := f.Approve(v["url"].(string), "carol")
	c.do("POST", "/api/logout", map[string]any{})
	resp, _ := c.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=error&reason=expired")

	if resp, _ := b.do("POST", "/api/fedi/unlink", map[string]any{}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlink: %d", resp.StatusCode)
	}
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type avatarSet struct {
	name       string
	onlyIfNone bool
	width      int
}

// recordAvatars records the avatars set (see Service.SetAvatar); done waits
// for the downloads and gives them.
func recordAvatars(t *testing.T, a *Service) (done func() []avatarSet) {
	var mu sync.Mutex
	var got []avatarSet
	a.SetAvatar = func(acc store.Account, avatar []byte, onlyIfNone bool) {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(avatar))
		if err != nil || format != "jpeg" || cfg.Height != cfg.Width {
			t.Errorf("avatar: %v %s %v", cfg, format, err)
		}
		mu.Lock()
		defer mu.Unlock()
		got = append(got, avatarSet{acc.Name, onlyIfNone, cfg.Width})
	}
	return func() []avatarSet {
		a.Fedi.avatars.Wait()
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
}

// A new account gets the remote account's avatar; a linked one only if it
// has none (the Mumble server keeps one it has).
func TestFediAvatar(t *testing.T) {
	f := &fedifake.Instance{}
	f.SetAvatar(pngOf(t, 300, 200))
	a, srv, inst := fediSetup(t, f)
	done := recordAvatars(t, a)
	b := newBrowser(t, srv)
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": inst}, "Alice"), home+"?fedi=register")
	if got := done(); len(got) != 0 {
		t.Fatalf("before the sign-up: %v", got)
	}
	b.do("POST", "/api/fedi/signup", map[string]any{"name": "alice"})
	if got := done(); !slices.Equal(got, []avatarSet{{"alice", false, 128}}) {
		t.Fatalf("sign-up: %v", got)
	}
	// Logins do not change it.
	wantRedirect(t, newBrowser(t, srv).fediFlow(f, map[string]any{"instance": inst}, "Alice"), home)
	if got := done(); len(got) != 0 {
		t.Fatalf("login: %v", got)
	}

	c := newBrowser(t, srv)
	c.do("POST", "/api/accounts", map[string]any{"name": "bob", "password": "longpassword"})
	wantRedirect(t, c.fediFlow(f, map[string]any{"instance": inst, "link": true, "password": "longpassword"}, "bobby"), home+"?fedi=linked")
	if got := done(); !slices.Equal(got, []avatarSet{{"bob", true, 128}}) {
		t.Fatalf("link: %v", got)
	}
}

// Broken or oversized pictures, or none: no avatar, the login goes on.
func TestFediAvatarFailures(t *testing.T) {
	for name, data := range map[string][]byte{
		"none":      nil,
		"broken":    []byte("<html>"),
		"oversized": pngOf(t, 9000, 1),
		"too large": append(pngOf(t, 10, 10), make([]byte, 3<<20)...),
	} {
		f := &fedifake.Instance{}
		f.SetAvatar(data)
		a, srv, inst := fediSetup(t, f)
		done := recordAvatars(t, a)
		b := newBrowser(t, srv)
		wantRedirect(t, b.fediFlow(f, map[string]any{"instance": inst}, "carol"), home+"?fedi=register")
		if resp, _ := b.do("POST", "/api/fedi/signup", map[string]any{"name": "carol"}); resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s: sign-up %d", name, resp.StatusCode)
		}
		if got := done(); len(got) != 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// Unlink, but not the last way to log in.
func TestFediUnlinkKeepsAWayToLogIn(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	d := newBrowser(t, srv)
	wantRedirect(t, d.fediFlow(f, map[string]any{"instance": inst}, "dave"), home+"?fedi=register")
	d.do("POST", "/api/fedi/signup", map[string]any{"name": "dave"})
	if resp, v := d.do("POST", "/api/fedi/unlink", map[string]any{}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("unlink the only login: %d %v", resp.StatusCode, v)
	}
	// A first password needs no old one (but a fresh fediverse login).
	wantRedirect(t, d.fediFlow(f, map[string]any{"instance": inst, "purpose": "reauth"}, "dave"), home+"?fedi=reauth")
	if resp, v := d.do("POST", "/api/password", map[string]any{"old": "", "new": "davespassword"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("first password: %d %v", resp.StatusCode, v)
	}
	if resp, _ := d.do("POST", "/api/fedi/unlink", map[string]any{}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlink with a password: %d", resp.StatusCode)
	}
	if resp, _ := d.do("POST", "/api/login", map[string]any{"name": "dave", "password": "davespassword"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("password login: %d", resp.StatusCode)
	}
}

func TestFediGuestAccountsLinkAndStay(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	a.GuestAccounts = true
	var kept string
	a.OnPermanent = func(name string) { kept = name }
	b := newBrowser(t, srv)
	b.do("POST", "/api/guest", map[string]any{"name": "visitor"})
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": inst, "link": true}, "visitor"), home+"?fedi=linked")
	if _, v := b.do("GET", "/api/me", nil); v["temporary"] != false || kept != "visitor" {
		t.Fatalf("guest after link: %v %q", v, kept)
	}
}

func TestFediHandleIsOptIn(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	b := newBrowser(t, srv)
	b.fediFlow(f, map[string]any{"instance": inst}, "erin")
	b.do("POST", "/api/fedi/signup", map[string]any{"name": "erin"})
	other := newBrowser(t, srv)
	if _, v := other.do("GET", "/api/fedi/handle?name=Erin", nil); v["handle"] != "" {
		t.Fatalf("before opting in: %v", v)
	}
	if resp, _ := other.do("POST", "/api/fedi/public", map[string]any{"on": true}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("public without login: %d", resp.StatusCode)
	}
	if resp, _ := b.do("POST", "/api/fedi/public", map[string]any{"on": true}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("public: %d", resp.StatusCode)
	}
	if _, v := other.do("GET", "/api/fedi/handle?name=Erin", nil); v["handle"] != "@erin@"+host {
		t.Fatalf("after opting in: %v", v)
	}
	if _, v := b.do("GET", "/api/me", nil); v["fedi"].(map[string]any)["public"] != true {
		t.Fatalf("me: %v", v)
	}
	b.do("POST", "/api/fedi/public", map[string]any{"on": false})
	if _, v := other.do("GET", "/api/fedi/handle?name=Erin", nil); v["handle"] != "" {
		t.Fatalf("after opting out: %v", v)
	}
}

func TestFediSignupNeedsTheBrowserCheck(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	a.Captcha = func() string { return "normal" }
	b := newBrowser(t, srv)
	b.fediFlow(f, map[string]any{"instance": inst}, "frank")
	if resp, _ := b.do("POST", "/api/fedi/signup", map[string]any{"name": "frank"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("without the check: %d", resp.StatusCode)
	}
	challenge, bits := getChallenge(t, srv.URL)
	resp, v := b.do("POST", "/api/fedi/signup", map[string]any{"name": "frank", "challenge": challenge, "nonce": solve(challenge, bits)})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("with the check: %d %v", resp.StatusCode, v)
	}
}

// Instances may delete apps nobody used: until a login goes through, an
// app is registered again after a while.
func TestFediAppsAreKeptOnceUsed(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	now := time.Now()
	a.Now = func() time.Time { return now }
	b := newBrowser(t, srv)
	b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	registered := f.Requests()
	b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	if f.Requests() != registered {
		t.Fatal("registered again at once")
	}
	// Pleroma deletes apps without a login after 15 minutes.
	now = now.Add(6 * time.Minute)
	b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	if f.Requests() == registered {
		t.Fatal("an unused app of 6 minutes was kept")
	}
	now = now.Add(2 * time.Hour)
	b.fediFlow(f, map[string]any{"instance": inst}, "gina") // registers again, then is used
	used := f.Requests()
	now = now.Add(48 * time.Hour)
	b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	if f.Requests() != used || used <= registered+1 {
		t.Fatalf("requests: %d %d %d", registered, used, f.Requests())
	}
}

// Misskey and Sharkey: one login uses an app up, so each login has its own.
func TestFediSingleUseAppsAreNotReused(t *testing.T) {
	f := &fedifake.Instance{SingleUseApps: true}
	_, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": inst}, "rain"), home+"?fedi=register")
	wantRedirect(t, newBrowser(t, srv).fediFlow(f, map[string]any{"instance": inst}, "rain"), home+"?fedi=register")
}

// The first password of a fediverse-only account needs a fresh login with
// the linked fediverse account, in the same session: a stolen session
// cannot set one and lock the owner out.
func TestFirstPasswordNeedsAFreshFediLogin(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	d := newBrowser(t, srv)
	d.fediFlow(f, map[string]any{"instance": inst}, "dave")
	d.do("POST", "/api/fedi/signup", map[string]any{"name": "dave"})
	setPassword := func(b *browser) (*http.Response, map[string]any) {
		return b.do("POST", "/api/password", map[string]any{"old": "", "new": "davespassword"})
	}
	if resp, v := setPassword(d); resp.StatusCode != http.StatusForbidden || v["code"] != "reauth" {
		t.Fatalf("without a fresh login: %d %v", resp.StatusCode, v)
	}
	// Another fediverse account proves nothing.
	reauth := map[string]any{"instance": inst, "purpose": "reauth"}
	wantRedirect(t, d.fediFlow(f, reauth, "eve"), home+"?fedi=error&reason=other")
	if resp, _ := setPassword(d); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("after another account: %d", resp.StatusCode)
	}
	// The proof belongs to the session that got it.
	wantRedirect(t, d.fediFlow(f, reauth, "dave"), home+"?fedi=reauth")
	other := newBrowser(t, srv)
	other.fediFlow(f, map[string]any{"instance": inst}, "dave")
	if resp, _ := setPassword(other); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("another session: %d", resp.StatusCode)
	}
	if resp, v := setPassword(d); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("with a fresh login: %d %v", resp.StatusCode, v)
	}
}

func TestFediProofsAreSingleUse(t *testing.T) {
	var f FediLogin
	now := time.Now()
	f.prove("session", 1, now)
	if f.takeProof("session", 2, now) || f.takeProof("session", 1, now) {
		t.Fatal("a proof for another account, or used twice")
	}
	f.prove("session", 1, now)
	if !f.takeProof("session", 1, now) || f.takeProof("session", 1, now) {
		t.Fatal("not single use")
	}
}

func TestFediReauthExpiresAndNeedsALink(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	now := time.Now()
	a.Now = func() time.Time { return now }
	d := newBrowser(t, srv)
	d.fediFlow(f, map[string]any{"instance": inst}, "dave")
	d.do("POST", "/api/fedi/signup", map[string]any{"name": "dave"})
	wantRedirect(t, d.fediFlow(f, map[string]any{"instance": inst, "purpose": "reauth"}, "dave"), home+"?fedi=reauth")
	now = now.Add(6 * time.Minute)
	if resp, v := d.do("POST", "/api/password", map[string]any{"new": "davespassword"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expired proof: %d %v", resp.StatusCode, v)
	}
	// Accounts with a password change it as before; a reauth needs a link.
	b := newBrowser(t, srv)
	b.do("POST", "/api/accounts", map[string]any{"name": "bob", "password": "longpassword"})
	if resp, _ := b.do("POST", "/api/fedi/start", map[string]any{"instance": inst, "purpose": "reauth"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("reauth without a link: %d", resp.StatusCode)
	}
	if resp, _ := b.do("POST", "/api/password", map[string]any{"old": "longpassword", "new": "newpassword"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("password change: %d", resp.StatusCode)
	}
}

// A new account from a fediverse login is made on our own site only, as
// other new accounts.
func TestFediSignupOnlyOnTheOwnSite(t *testing.T) {
	_, srv, _ := fediSetup(t, &fedifake.Instance{})
	req, _ := http.NewRequest("POST", srv.URL+"/api/fedi/signup", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://other.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("from another site: %d, want 403", resp.StatusCode)
	}
}

// Cookies of the old path (/api/fedi) go when a flow starts: over plain
// HTTP they have the same name and the browser sends them first.
func TestFediStartRemovesOldPathCookies(t *testing.T) {
	_, srv, inst := fediSetup(t, &fedifake.Instance{})
	b := newBrowser(t, srv)
	resp, _ := b.do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	gone := map[string]bool{}
	for _, c := range resp.Cookies() {
		if c.Path == fediPath && c.MaxAge < 0 {
			gone[c.Name] = true
		}
	}
	if !gone[stateCookie] || !gone[signupCookie] {
		t.Fatalf("cookies %v", resp.Cookies())
	}
}

// An admin's lists (see package homes): a blocked home server's accounts
// cannot start a login, nor finish one started before the block.
func TestFediLoginFromABlockedServer(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, instURL := fediSetup(t, f)
	host := strings.TrimPrefix(instURL, "https://")
	blocked := true
	a.Fedi.AllowsHome = func(h string) bool { return !(blocked && h == host) }
	b := newBrowser(t, srv)
	if resp, v := b.do("POST", "/api/fedi/start", map[string]any{"instance": host}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("start: %d %v", resp.StatusCode, v)
	}
	blocked = false
	resp, v := b.do("POST", "/api/fedi/start", map[string]any{"instance": host})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start: %d %v", resp.StatusCode, v)
	}
	blocked = true
	callback, err := f.Approve(v["url"].(string), "alice")
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = b.do("GET", callback, nil)
	wantRedirect(t, resp, srv.URL+"/?fedi=error&reason=blocked")
}

// Handoff (another server's page adds this one, in a popup): no cookie
// here; the token once, for the provider's origin only.
func TestFediHandoff(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	b := newBrowser(t, srv)
	handoff := func() map[string]any {
		resp, v := b.do("POST", "/api/fedi/handoff", map[string]any{})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("handoff: %d %v", resp.StatusCode, v)
		}
		return v
	}
	session := func() bool {
		_, v := b.do("GET", "/api/me", nil)
		return v["name"] != nil
	}
	// A new account: the name first, then the token.
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": host, "handoff": true}, "Alice"), home+"?fedi=register")
	resp, v := b.do("POST", "/api/fedi/signup", map[string]any{"name": "alice"})
	if resp.StatusCode != http.StatusCreated || v["handoff"] != true {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
	if session() {
		t.Fatal("a handoff signup set a session cookie")
	}
	v = handoff()
	if v["name"] != "alice" || v["origin"] != inst || v["token"] == "" {
		t.Fatalf("handoff: %v", v)
	}
	// Once only.
	if resp, _ := b.do("POST", "/api/fedi/handoff", map[string]any{}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a second handoff: %d", resp.StatusCode)
	}
	// The token is a session here.
	req, _ := http.NewRequest("GET", srv.URL+"/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+v["token"].(string))
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("the token: %v %v", resp, err)
	}
	// A known account: back with the token at once, still no cookie.
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": host, "handoff": true}, "Alice"), home+"?fedi=connect")
	if session() {
		t.Fatal("a handoff login set a session cookie")
	}
	if v := handoff(); v["name"] != "alice" {
		t.Fatalf("handoff again: %v", v)
	}
	// Not with a link or a reauth.
	if resp, _ := b.do("POST", "/api/fedi/start", map[string]any{"instance": host, "handoff": true, "link": true}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("handoff and link: %d", resp.StatusCode)
	}
}

// A handoff to a page other than the login's server (the home server of
// an account named after its fediverse home adds this one): the token
// only after a yes on this server's page, for that page's exact origin.
func TestFediHandoffToAnotherPage(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	to := "https://home.example"
	b := newBrowser(t, srv)
	start := map[string]any{"instance": host, "handoff": true, "to": to}
	wantRedirect(t, b.fediFlow(f, start, "Alice"), home+"?fedi=register")
	if resp, v := b.do("POST", "/api/fedi/signup", map[string]any{"name": "alice"}); resp.StatusCode != http.StatusCreated || v["handoff"] != true {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
	// The question first: no token.
	resp, v := b.do("POST", "/api/fedi/handoff", map[string]any{})
	if resp.StatusCode != http.StatusOK || v["ask"] == nil || v["ask"] == "" || v["name"] != "alice" || v["origin"] != to || v["token"] != nil {
		t.Fatalf("handoff without a yes: %d %v", resp.StatusCode, v)
	}
	ask := v["ask"]
	// A yes to another question: nothing.
	if resp, v := b.do("POST", "/api/fedi/handoff", map[string]any{"allow": "x"}); resp.StatusCode != http.StatusForbidden || v["token"] != nil {
		t.Fatalf("a yes to another question: %d %v", resp.StatusCode, v)
	}
	resp, v = b.do("POST", "/api/fedi/handoff", map[string]any{"allow": ask})
	if resp.StatusCode != http.StatusOK || v["name"] != "alice" || v["origin"] != to || v["token"] == "" {
		t.Fatalf("handoff with a yes: %d %v", resp.StatusCode, v)
	}
	if resp, _ := b.do("POST", "/api/fedi/handoff", map[string]any{"allow": ask}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a second handoff: %d", resp.StatusCode)
	}
	// A known account asks too; a yes counts for the question shown, not
	// for a login that replaced it meanwhile.
	wantRedirect(t, b.fediFlow(f, start, "Alice"), home+"?fedi=connect")
	_, v = b.do("POST", "/api/fedi/handoff", map[string]any{})
	if v["ask"] == nil || v["token"] != nil {
		t.Fatalf("known account without a yes: %v", v)
	}
	shown := v["ask"]
	wantRedirect(t, b.fediFlow(f, start, "Bob"), home+"?fedi=register")
	b.do("POST", "/api/fedi/signup", map[string]any{"name": "bob"})
	if resp, v := b.do("POST", "/api/fedi/handoff", map[string]any{"allow": shown}); resp.StatusCode != http.StatusForbidden || v["token"] != nil {
		t.Fatalf("a yes for a replaced login: %d %v", resp.StatusCode, v)
	}
	// The login's own server asks nothing (as without "to").
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": host, "handoff": true, "to": inst}, "Alice"), home+"?fedi=connect")
	if _, v := b.do("POST", "/api/fedi/handoff", map[string]any{}); v["ask"] != nil || v["token"] == "" || v["origin"] != inst {
		t.Fatalf("handoff to the login's server: %v", v)
	}
}

// A handoff's page: only an origin (on any port), and only with a handoff.
func TestFediHandoffPageIsAnOrigin(t *testing.T) {
	f := &fedifake.Instance{}
	_, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	to := "https://home.example"
	b := newBrowser(t, srv)
	for _, bad := range []map[string]any{
		{"instance": host, "handoff": true, "to": to + "/path"},
		{"instance": host, "handoff": true, "to": "home.example"},
		{"instance": host, "handoff": true, "to": "https://user@home.example"},
		{"instance": host, "handoff": true, "to": "ftp://home.example"},
		{"instance": host, "to": to},
	} {
		if resp, _ := b.do("POST", "/api/fedi/start", bad); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("start %v: %d", bad, resp.StatusCode)
		}
	}
	if resp, v := b.do("POST", "/api/fedi/start", map[string]any{"instance": host, "handoff": true, "to": "https://home.example:8443"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("a home on another port: %d %v", resp.StatusCode, v)
	}
}

// Without a name, the account gets its remote one (name@home, as the
// fediverse writes it); taken: the name step.
func TestFediSignupWithTheRemoteName(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	b := newBrowser(t, srv)
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": host}, "Alice"), home+"?fedi=register")
	if _, v := b.do("GET", "/api/fedi/signup", nil); v["remote"] != "Alice@"+host {
		t.Fatalf("signup info: %v", v)
	}
	resp, v := b.do("POST", "/api/fedi/signup", map[string]any{})
	if resp.StatusCode != http.StatusCreated || v["name"] != "Alice@"+host {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
	// Named after its home: it stays linked.
	if resp, _ := b.do("POST", "/api/fedi/unlink", map[string]any{}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("unlink of a remote name: %d", resp.StatusCode)
	}
	// Taken already (an account of that name from before): the name step.
	a.Store.CreateAccount(store.Account{Name: "Bob@" + host, PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, time.Now(), "")
	b2 := newBrowser(t, srv)
	wantRedirect(t, b2.fediFlow(f, map[string]any{"instance": host}, "Bob"), home+"?fedi=register")
	if resp, _ := b2.do("POST", "/api/fedi/signup", map[string]any{}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("a taken remote name: %d", resp.StatusCode)
	}
	if resp, v := b2.do("POST", "/api/fedi/signup", map[string]any{"name": "bob2"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("the name step: %d %v", resp.StatusCode, v)
	}
	// Names of here take no "@": nobody poses as a remote account.
	if resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "bob@" + host, "password": "longpassword"}, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a local name with @: %d", resp.StatusCode)
	}
}

// A known account's confirmed home domain counts for the block list, also
// when today's handle lookup gives only the instance's host.
func TestFediBlockUsesTheKnownDomain(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	b := newBrowser(t, srv)
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": host}, "Alice"), home+"?fedi=register")
	b.do("POST", "/api/fedi/signup", map[string]any{})
	b.do("POST", "/api/logout", map[string]any{})
	acc, _ := a.Store.AccountByName("Alice@" + host)
	id, _ := a.Store.FediIdentityOf(acc.ID)
	a.Store.UpdateFediHandle(id.Host, id.RemoteID, "@alice@blocked.example") // confirmed at an earlier login
	a.Fedi.AllowsHome = func(h string) bool { return h != "blocked.example" }
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": host}, "Alice"), home+"?fedi=error&reason=blocked")
	if id, _ := a.Store.FediIdentityOf(acc.ID); id.Handle != "@alice@blocked.example" {
		t.Fatalf("the confirmed handle changed: %q", id.Handle)
	}
}

// Blocking a home server ends the logins of the accounts that log in only
// with it (their connections too); others stay.
func TestEndBlockedHomes(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	b := newBrowser(t, srv)
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": host}, "Alice"), home+"?fedi=register")
	if resp, v := b.do("POST", "/api/fedi/signup", map[string]any{"name": "alice"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
	loggedIn := func() bool {
		_, v := b.do("GET", "/api/me", nil)
		return v["name"] == "alice"
	}
	var revoked []int64
	a.Revoke = func(match func(int64, string) bool) {
		for id := int64(1); id < 10; id++ {
			if match(id, "") {
				revoked = append(revoked, id)
			}
		}
	}
	// An account with a password, linked to the same home: it stays.
	now := time.Now()
	bob, _ := a.Store.CreateAccount(store.Account{Name: "bob", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	a.Store.CreateSession(tokenHash("bob"), bob.ID, now.Add(time.Hour))
	if err := a.Store.LinkFedi(bob.ID, tokenHash("bob"), store.FediIdentity{Host: host, RemoteID: "bob-id", Handle: "@bob@" + host}, now); err != nil {
		t.Fatal(err)
	}
	var keysEnded []string
	a.StreamKeyChanged = func(name string) { keysEnded = append(keysEnded, name) }
	blocked := false
	a.Fedi.AllowsHome = func(h string) bool { return !(blocked && h == host) }
	a.EndBlockedHomes()
	if !loggedIn() || len(revoked) != 0 {
		t.Fatalf("not blocked: logged in %v, revoked %v", loggedIn(), revoked)
	}
	blocked = true
	a.EndBlockedHomes()
	if loggedIn() || len(revoked) != 1 {
		t.Fatalf("blocked: logged in %v, revoked %v", loggedIn(), revoked)
	}
	if len(keysEnded) != 1 || keysEnded[0] != "alice" {
		t.Fatalf("stream keys ended: %v", keysEnded)
	}
	if _, err := a.Store.AccountBySession(tokenHash("bob"), time.Now()); err != nil {
		t.Fatalf("the account with a password was logged out: %v", err)
	}
}

// A handoff waiting when the admins block its home server is not taken.
func TestFediHandoffAfterABlock(t *testing.T) {
	f := &fedifake.Instance{}
	a, srv, inst := fediSetup(t, f)
	host := strings.TrimPrefix(inst, "https://")
	blocked := false
	a.Fedi.AllowsHome = func(h string) bool { return !(blocked && h == host) }
	b := newBrowser(t, srv)
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": host, "handoff": true}, "Alice"), home+"?fedi=register")
	if resp, v := b.do("POST", "/api/fedi/signup", map[string]any{"name": "alice"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
	blocked = true
	if resp, v := b.do("POST", "/api/fedi/handoff", map[string]any{}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("handoff after the block: %d %v", resp.StatusCode, v)
	}
}
