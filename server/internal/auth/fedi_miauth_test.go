package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/fedi/fedifake"
	"kallisti.chat/server/internal/store"
)

func startMiAuth(t *testing.T, b *browser, start map[string]any) *url.URL {
	t.Helper()
	resp, v := b.do("POST", "/api/fedi/start", start)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("MiAuth start: %d %v", resp.StatusCode, v)
	}
	authorize, err := url.Parse(v["url"].(string))
	if err != nil || !strings.HasPrefix(authorize.Path, "/miauth/") {
		t.Fatalf("MiAuth authorization URL: %v %v", authorize, err)
	}
	q := authorize.Query()
	if q.Get("permission") != "read:account" || q.Get("name") != "Kallisti" || q.Get("scope") != "" || q.Get("client_id") != "" {
		t.Fatalf("MiAuth parameters: %v", q)
	}
	callback, err := url.Parse(q.Get("callback"))
	if err != nil || callback.Scheme+"://"+callback.Host+callback.Path != b.srv.URL+"/api/fedi/callback" || callback.Query().Get("state") == "" {
		t.Fatalf("MiAuth callback: %v %v", callback, err)
	}
	return authorize
}

func TestFediMiAuthLinkAndLogIn(t *testing.T) {
	f := &fedifake.Instance{NativeMisskey: true}
	a, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	if resp, v := b.do("POST", "/api/accounts", map[string]any{"name": "local", "password": "local-password"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("account: %d %v", resp.StatusCode, v)
	}
	link := map[string]any{"instance": inst, "link": true, "password": "wrong-password"}
	if resp, _ := b.do("POST", "/api/fedi/start", link); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	link["password"] = "local-password"
	if resp, _ := newBrowser(t, srv).do("POST", "/api/fedi/start", link); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no session: %d", resp.StatusCode)
	}
	authorize := startMiAuth(t, b, link)
	callback, err := f.Approve(authorize.String(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := b.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=linked")
	if _, v := b.do("GET", "/api/me", nil); v["name"] != "local" || v["fedi"].(map[string]any)["handle"] != "@alice@"+strings.TrimPrefix(inst, "https://") {
		t.Fatalf("linked account: %v", v)
	}
	if _, err := a.Store.FediApp(strings.TrimPrefix(inst, "https://"), a.Fedi.redirectURI()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("MiAuth saved an OAuth app: %v", err)
	}
	other := newBrowser(t, srv)
	next := startMiAuth(t, other, map[string]any{"instance": inst})
	if next.Path == authorize.Path {
		t.Fatal("MiAuth session was reused")
	}
	callback, err = f.Approve(next.String(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = other.do("GET", callback, nil)
	wantRedirect(t, resp, home)
	if _, v := other.do("GET", "/api/me", nil); v["name"] != "local" {
		t.Fatalf("login after linking: %v", v)
	}
	c := newBrowser(t, srv)
	c.do("POST", "/api/accounts", map[string]any{"name": "other", "password": "local-password"})
	wantRedirect(t, c.fediFlow(f, link, "alice"), home+"?fedi=error&reason=taken")
	if resp, _ := b.do("POST", "/api/fedi/unlink", map[string]any{}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlink: %d", resp.StatusCode)
	}
}

func TestFediMiAuthStateAndReplay(t *testing.T) {
	f := &fedifake.Instance{NativeMisskey: true}
	_, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	authorize := startMiAuth(t, b, map[string]any{"instance": inst})
	callback, err := f.Approve(authorize.String(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := url.Parse(callback)
	app, _ := url.Parse(srv.URL)
	cookies := b.c.Jar.Cookies(app)
	requests := f.Requests()
	other := newBrowser(t, srv)
	resp, _ := other.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=error&reason=expired")
	for _, state := range []string{"", "wrong"} {
		q := cb.Query()
		q.Set("state", state)
		resp, _ = b.do("GET", "/api/fedi/callback?"+q.Encode(), nil)
		wantRedirect(t, resp, home+"?fedi=error&reason=expired")
	}
	if f.Requests() != requests {
		t.Fatal("invalid browser state reached the instance")
	}
	resp, _ = b.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=register")
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("callback can leak credentials")
	}
	// Restoring the state cookie cannot resurrect the consumed flow.
	b.c.Jar.SetCookies(app, cookies)
	requests = f.Requests()
	resp, _ = b.do("GET", callback, nil)
	wantRedirect(t, resp, home+"?fedi=error&reason=expired")
	if f.Requests() != requests {
		t.Fatal("replay reached the instance")
	}
}

func TestFediMiAuthLinkBoundaries(t *testing.T) {
	for _, scenario := range []string{"missing-session", "other-session", "expired", "changed-login", "blocked", "denied", "invalid-scope", "unapproved"} {
		t.Run(scenario, func(t *testing.T) {
			f := &fedifake.Instance{NativeMisskey: true}
			a, srv, inst := fediSetup(t, f)
			b := newBrowser(t, srv)
			if resp, v := b.do("POST", "/api/accounts", map[string]any{"name": "local", "password": "local-password"}); resp.StatusCode != http.StatusCreated {
				t.Fatalf("account: %d %v", resp.StatusCode, v)
			}
			authorize := startMiAuth(t, b, map[string]any{"instance": inst, "link": true, "password": "local-password"})
			callback := withQuery(authorize.Query().Get("callback"), url.Values{"session": {strings.TrimPrefix(authorize.Path, "/miauth/")}})
			if scenario != "unapproved" {
				var err error
				callback, err = f.Approve(authorize.String(), "alice")
				if err != nil {
					t.Fatal(err)
				}
			}
			cb, _ := url.Parse(callback)
			q := cb.Query()
			reason := "expired"
			switch scenario {
			case "missing-session":
				q.Del("session")
			case "other-session":
				attacker := startMiAuth(t, newBrowser(t, srv), map[string]any{"instance": inst})
				if _, err := f.Approve(attacker.String(), "mallory"); err != nil {
					t.Fatal(err)
				}
				q.Set("session", strings.TrimPrefix(attacker.Path, "/miauth/"))
			case "expired":
				now := a.Now().Add(flowLifetime)
				a.Now = func() time.Time { return now }
			case "changed-login":
				b.do("POST", "/api/logout", map[string]any{})
				if resp, v := b.do("POST", "/api/login", map[string]any{"name": "local", "password": "local-password"}); resp.StatusCode != http.StatusOK {
					t.Fatalf("replacement session: %d %v", resp.StatusCode, v)
				}
			case "blocked":
				a.Fedi.AllowsHome = func(string) bool { return false }
				reason = "blocked"
			case "denied", "invalid-scope":
				reason = "denied"
				q.Set("error", "access_denied")
				if scenario == "invalid-scope" {
					q.Set("error", "invalid_scope")
				}
			case "unapproved":
				reason = "unreachable"
			}
			cb.RawQuery = q.Encode()
			requests := f.Requests()
			resp, _ := b.do("GET", cb.String(), nil)
			wantRedirect(t, resp, home+"?fedi=error&reason="+reason)
			if scenario != "unapproved" && f.Requests() != requests {
				t.Fatal("invalid callback reached the instance")
			}
			acc, err := a.Store.AccountByName("local")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.Store.FediIdentityOf(acc.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("failed callback linked an identity: %v", err)
			}
		})
	}
}

func TestFediMiAuthDelegationAndReauth(t *testing.T) {
	a, srv, f, inst, domain := delegatedFediSetup(t)
	f.NativeMisskey = true
	b := newBrowser(t, srv)
	// The approved identity, not the typed handle, owns the new account.
	wantRedirect(t, b.fediFlow(f, map[string]any{"instance": "@alice@" + domain}, "bob"), home+"?fedi=register")
	if resp, v := b.do("POST", "/api/fedi/signup", map[string]any{"name": "bob"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
	acc, err := a.Store.AccountByName("bob")
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.FediIdentityOf(acc.ID)
	if err != nil || id.Host != strings.TrimPrefix(inst, "https://") || id.Handle != "@bob@"+domain {
		t.Fatalf("delegated identity: %+v %v", id, err)
	}
	reauth := map[string]any{"instance": domain, "purpose": "reauth"}
	wantRedirect(t, b.fediFlow(f, reauth, "alice"), home+"?fedi=error&reason=other")
	if resp, _ := b.do("POST", "/api/password", map[string]any{"new": "bobs-password"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong identity authorized a password: %d", resp.StatusCode)
	}
	// Reauth uses the stored API host, not the discovered display domain.
	wantRedirect(t, b.fediFlow(f, reauth, "bob"), home+"?fedi=reauth")
	other := newBrowser(t, srv)
	wantRedirect(t, other.fediFlow(f, map[string]any{"instance": inst}, "bob"), home)
	if resp, _ := other.do("POST", "/api/password", map[string]any{"new": "bobs-password"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("another session used the reauth proof: %d", resp.StatusCode)
	}
	if resp, v := b.do("POST", "/api/password", map[string]any{"new": "bobs-password"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("first password: %d %v", resp.StatusCode, v)
	}
}

func TestFediMiAuthHandoff(t *testing.T) {
	f := &fedifake.Instance{NativeMisskey: true}
	_, srv, inst := fediSetup(t, f)
	owner := newBrowser(t, srv)
	wantRedirect(t, owner.fediFlow(f, map[string]any{"instance": inst}, "alice"), home+"?fedi=register")
	if resp, v := owner.do("POST", "/api/fedi/signup", map[string]any{"name": "alice"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup: %d %v", resp.StatusCode, v)
	}
	popup := newBrowser(t, srv)
	origin := "https://home.example"
	resp := popup.fediFlow(f, map[string]any{"instance": inst, "handoff": true, "to": origin}, "alice")
	wantRedirect(t, resp, home+"?fedi=connect")
	if sessionCookie(resp) != nil {
		t.Fatal("handoff logged the browser in")
	}
	resp, v := popup.do("POST", "/api/fedi/handoff", map[string]any{})
	if resp.StatusCode != http.StatusOK || v["ask"] == nil || v["token"] != nil || v["origin"] != origin {
		t.Fatalf("handoff consent: %d %v", resp.StatusCode, v)
	}
	resp, v = popup.do("POST", "/api/fedi/handoff", map[string]any{"allow": v["ask"]})
	if resp.StatusCode != http.StatusOK || v["token"] == nil || v["name"] != "alice" || v["origin"] != origin {
		t.Fatalf("handoff: %d %v", resp.StatusCode, v)
	}
	if resp, _ := popup.do("POST", "/api/fedi/handoff", map[string]any{}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("handoff replay: %d", resp.StatusCode)
	}
}

func TestFediOAuthDoesNotRequireMiAuthDiscovery(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		f := &fedifake.Instance{}
		_, srv, inst := fediSetup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/meta" {
				t.Error("working OAuth server received a MiAuth discovery request")
				if malformed {
					w.Write([]byte("not JSON"))
				} else {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
				return
			}
			f.ServeHTTP(w, r)
		}))
		resp, _ := newBrowser(t, srv).do("POST", "/api/fedi/start", map[string]any{"instance": inst})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("MiAuth discovery interfered with OAuth: %d", resp.StatusCode)
		}
	}
}
