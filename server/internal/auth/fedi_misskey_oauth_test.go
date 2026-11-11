package auth

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"kallisti.chat/server/internal/fedi"
	"kallisti.chat/server/internal/fedi/fedifake"
	"kallisti.chat/server/internal/store"
)

func startMisskeyOAuth(t *testing.T, b *browser, start map[string]any) *url.URL {
	t.Helper()
	resp, v := b.do("POST", "/api/fedi/start", start)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Misskey OAuth start: %d %v", resp.StatusCode, v)
	}
	authorize, err := url.Parse(v["url"].(string))
	if err != nil || authorize.Path != "/oauth/authorize" {
		t.Fatalf("Misskey authorization URL: %v %v", authorize, err)
	}
	q := authorize.Query()
	if q.Get("scope") != "read:account" || q.Get("client_id") != b.srv.URL+"/api/fedi/client" ||
		q.Get("response_type") != "code" || q.Get("state") == "" ||
		q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 || q.Get("client_secret") != "" {
		t.Fatalf("Misskey OAuth parameters: %v", q)
	}
	callback, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || callback.String() != b.srv.URL+"/api/fedi/callback" {
		t.Fatalf("Misskey callback: %v %v", callback, err)
	}
	return authorize
}

func TestFediMisskeyOAuthLinkAndLogIn(t *testing.T) {
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
	authorize := startMisskeyOAuth(t, b, link)
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
		t.Fatalf("native public client saved a registered OAuth app: %v", err)
	}
	other := newBrowser(t, srv)
	next := startMisskeyOAuth(t, other, map[string]any{"instance": inst})
	if next.Query().Get("state") == authorize.Query().Get("state") || next.Query().Get("code_challenge") == authorize.Query().Get("code_challenge") {
		t.Fatal("OAuth state or verifier was reused")
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

func TestFediMisskeyOAuthStateAndReplay(t *testing.T) {
	f := &fedifake.Instance{NativeMisskey: true}
	_, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	authorize := startMisskeyOAuth(t, b, map[string]any{"instance": inst})
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

func TestFediMisskeyOAuthLinkBoundaries(t *testing.T) {
	for _, scenario := range []string{"missing-code", "other-code", "missing-issuer", "other-issuer", "duplicate-state", "duplicate-code", "duplicate-issuer", "expired", "changed-login", "blocked", "denied", "invalid-scope", "unapproved"} {
		t.Run(scenario, func(t *testing.T) {
			f := &fedifake.Instance{NativeMisskey: true}
			a, srv, inst := fediSetup(t, f)
			b := newBrowser(t, srv)
			if resp, v := b.do("POST", "/api/accounts", map[string]any{"name": "local", "password": "local-password"}); resp.StatusCode != http.StatusCreated {
				t.Fatalf("account: %d %v", resp.StatusCode, v)
			}
			authorize := startMisskeyOAuth(t, b, map[string]any{"instance": inst, "link": true, "password": "local-password"})
			callback := withQuery(authorize.Query().Get("redirect_uri"), url.Values{
				"state": {authorize.Query().Get("state")}, "code": {"unapproved"}, "iss": {inst},
			})
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
			case "missing-code":
				q.Del("code")
				reason = "denied"
			case "other-code":
				attacker := startMisskeyOAuth(t, newBrowser(t, srv), map[string]any{"instance": inst})
				other, err := f.Approve(attacker.String(), "mallory")
				if err != nil {
					t.Fatal(err)
				}
				otherCallback, _ := url.Parse(other)
				q.Set("code", otherCallback.Query().Get("code"))
				reason = "unreachable" // the other flow's PKCE verifier cannot match
			case "missing-issuer":
				q.Del("iss")
			case "other-issuer":
				q.Set("iss", "https://other.example")
			case "duplicate-state":
				q.Add("state", q.Get("state"))
			case "duplicate-code":
				q.Add("code", q.Get("code"))
			case "duplicate-issuer":
				q.Add("iss", q.Get("iss"))
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
			if scenario != "unapproved" && scenario != "other-code" && f.Requests() != requests {
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

func TestFediMisskeyOAuthDelegationAndReauth(t *testing.T) {
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

func TestFediMisskeyOAuthHandoff(t *testing.T) {
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

func TestFediMastodonDoesNotRequireMisskeyDiscovery(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		f := &fedifake.Instance{}
		_, srv, inst := fediSetup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/oauth-authorization-server" {
				t.Error("working Mastodon OAuth server received native discovery")
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
			t.Fatalf("native discovery interfered with Mastodon OAuth: %d", resp.StatusCode)
		}
	}
}

func TestFediMisskeyClientMetadata(t *testing.T) {
	a, srv, _ := fediSetup(t, &fedifake.Instance{NativeMisskey: true})
	a.Fedi.AppName = `Kallisti <script>alert("x")</script> & friends`
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/fedi/client?redirect_uri=https://evil.example/callback", nil)
	req.Host = "evil.example"
	req.Header.Set("X-Forwarded-Host", "evil.example")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || len(resp.Cookies()) != 0 {
		t.Fatalf("public client metadata: %d %v", resp.StatusCode, resp.Header)
	}
	if strings.Contains(string(body), "evil.example") || strings.Contains(string(body), "<script>") || !strings.Contains(string(body), `class="h-app"`) {
		t.Fatalf("unsafe client metadata: %s", body)
	}
	z := html.NewTokenizer(strings.NewReader(string(body)))
	var redirects []string
	var name string
	inName := false
	for z.Next() != html.ErrorToken {
		token := z.Token()
		if token.Type == html.EndTagToken && token.Data == "a" {
			inName = false
		}
		if token.Type == html.TextToken && inName {
			name += token.Data
		}
		if token.Type != html.StartTagToken && token.Type != html.SelfClosingTagToken {
			continue
		}
		var rel, href, class string
		for _, attr := range token.Attr {
			switch attr.Key {
			case "rel":
				rel = attr.Val
			case "href":
				href = attr.Val
			case "class":
				class = attr.Val
			}
		}
		// Upstream only accepts an h-app name whose URL equals client_id.
		if token.Data == "a" && strings.Contains(" "+class+" ", " p-name ") && href == a.Fedi.clientID() {
			inName = true
		}
		if rel == "redirect_uri" {
			redirects = append(redirects, href)
		}
	}
	if len(redirects) != 1 || redirects[0] != srv.URL+"/api/fedi/callback" || name != a.Fedi.AppName {
		t.Fatalf("client metadata: redirects=%v name=%q", redirects, name)
	}
}

func TestFediMisskeyMetadataRedirectWhitelistAndPKCE(t *testing.T) {
	f := &fedifake.Instance{NativeMisskey: true}
	a, srv, inst := fediSetup(t, f)
	b := newBrowser(t, srv)
	authorize := startMisskeyOAuth(t, b, map[string]any{"instance": inst})
	for _, changes := range []url.Values{
		{"redirect_uri": {"https://evil.example/callback"}},
		{"redirect_uri": {srv.URL + "/api/fedi/callback?extra=1"}},
		{"code_challenge_method": {"plain"}},
		{"code_challenge": {""}},
		{"scope": {"read:account write:notes"}},
	} {
		q := authorize.Query()
		for key, values := range changes {
			q[key] = values
		}
		request := *authorize
		request.RawQuery = q.Encode()
		if _, err := f.Approve(request.String(), "alice"); err == nil {
			t.Fatalf("approved invalid authorization: %v", changes)
		}
	}
	// The provider really fetches client metadata and cannot follow redirects
	// to an attacker-controlled document or accept unavailable metadata.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, a.Fedi.clientID(), http.StatusFound)
	}))
	defer redirect.Close()
	for _, clientID := range []string{redirect.URL, srv.URL + "/missing-client"} {
		q := authorize.Query()
		q.Set("client_id", clientID)
		if _, err := f.Approve(withQuery(authorize.String(), q), "alice"); err == nil {
			t.Fatalf("approved unavailable or redirected client metadata: %s", clientID)
		}
	}
	flow, ok := a.Fedi.flows.get(authorize.Query().Get("state"), a.Now(), false)
	if !ok || flow.misskey == nil || fedi.Challenge(flow.verifier) != authorize.Query().Get("code_challenge") {
		t.Fatal("PKCE verifier not bound to the flow")
	}
	// Token grants bind the client, redirect and verifier, and are single-use.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // local httptest certificate
	defer client.CloseIdleConnections()
	for _, field := range []string{"client_id", "client_secret", "redirect_uri", "code_verifier", "replay"} {
		callback, err := f.Approve(authorize.String(), "alice")
		if err != nil {
			t.Fatal(err)
		}
		cb, _ := url.Parse(callback)
		form := url.Values{
			"grant_type": {"authorization_code"}, "code": {cb.Query().Get("code")},
			"client_id": {a.Fedi.clientID()}, "redirect_uri": {a.Fedi.redirectURI()}, "code_verifier": {flow.verifier},
		}
		if field != "replay" {
			form.Set(field, "wrong")
		}
		resp, err := client.PostForm(inst+"/oauth/token", form)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := http.StatusBadRequest
		if field == "replay" {
			want = http.StatusOK
		}
		if resp.StatusCode != want {
			t.Fatalf("%s token exchange: %d, want %d", field, resp.StatusCode, want)
		}
		resp, err = client.PostForm(inst+"/oauth/token", form)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s replay token exchange: %d", field, resp.StatusCode)
		}
	}
}

func TestFediMisskeyOptionalIssuer(t *testing.T) {
	for _, issuer := range []string{"", "matching", "https://other.example"} {
		t.Run(issuer, func(t *testing.T) {
			f := &fedifake.Instance{NativeMisskey: true, NoIssuer: true}
			_, srv, inst := fediSetup(t, f)
			b := newBrowser(t, srv)
			authorize := startMisskeyOAuth(t, b, map[string]any{"instance": inst})
			callback, err := f.Approve(authorize.String(), "alice")
			if err != nil {
				t.Fatal(err)
			}
			cb, _ := url.Parse(callback)
			if issuer != "" {
				value := issuer
				if value == "matching" {
					value = inst
				}
				callback = withQuery(callback, url.Values{"iss": {value}})
			} else if cb.Query().Has("iss") {
				t.Fatal("fake advertised an optional issuer but sent one")
			}
			requests := f.Requests()
			resp, _ := b.do("GET", callback, nil)
			if issuer == "https://other.example" {
				wantRedirect(t, resp, home+"?fedi=error&reason=expired")
				if f.Requests() != requests {
					t.Fatal("wrong issuer reached token endpoint")
				}
			} else {
				wantRedirect(t, resp, home+"?fedi=register")
			}
		})
	}
}

func TestFediMisskeyDiscoveryRequired(t *testing.T) {
	f := &fedifake.Instance{NativeMisskey: true}
	_, srv, inst := fediSetup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			http.NotFound(w, r)
		case "/api/meta":
			t.Error("unsupported OAuth fell back to obsolete native authentication")
			http.NotFound(w, r)
		default:
			f.ServeHTTP(w, r)
		}
	}))
	resp, _ := newBrowser(t, srv).do("POST", "/api/fedi/start", map[string]any{"instance": inst})
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("unsupported OAuth: %d", resp.StatusCode)
	}
	for _, cookie := range resp.Cookies() {
		if strings.Contains(cookie.Name, stateCookie) && cookie.Value != "" {
			t.Fatal("unsupported OAuth started a pending login")
		}
	}
}
