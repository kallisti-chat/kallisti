package fedi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"kallisti.chat/server/internal/fedi"
	"kallisti.chat/server/internal/fedi/fedifake"
)

func TestParseInstance(t *testing.T) {
	for in, want := range map[string]string{
		"mastodon.social":                  "mastodon.social",
		"  Mastodon.Social  ":              "mastodon.social",
		"https://mastodon.social/@me":      "mastodon.social",
		"https://mastodon.social:443/":     "mastodon.social",
		"mastodon.social/about?x=1#y":      "mastodon.social",
		"@alice@Mastodon.Social":           "mastodon.social",
		"alice@mastodon.social":            "mastodon.social",
		"https://user:pw@mastodon.social/": "mastodon.social",
		"bücher.example":                   "xn--bcher-kva.example",
		"social.example.":                  "social.example",
		"social.example。":                  "social.example",
	} {
		got, err := fedi.ParseInstance(in, false)
		if err != nil || got.Host != want || got.Base != "https://"+want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	for _, in := range []string{
		"", "localhost", "http://mastodon.social", "mastodon.social:8443", "127.0.0.1", "[::1]",
		"exa mple.org", "ftp://mastodon.social", "-bad-.example", "a..b",
		"social.example..", "social.example。.", // one root dot only (also an ideographic full stop)
	} {
		if got, err := fedi.ParseInstance(in, false); err == nil {
			t.Errorf("%q: accepted as %+v", in, got)
		}
	}
	// Development (FEDI_INSECURE_DEV_INSTANCES): local test instances.
	got, err := fedi.ParseInstance("http://127.0.0.1:9000/", true)
	if err != nil || got.Host != "127.0.0.1:9000" || got.Base != "http://127.0.0.1:9000" {
		t.Fatalf("dev: %+v %v", got, err)
	}
}

func TestPKCE(t *testing.T) {
	v := fedi.NewVerifier()
	if len(v) < 43 || v == fedi.NewVerifier() {
		t.Fatalf("verifier %q", v)
	}
	// Computed with Python (hashlib, urlsafe_b64encode without padding).
	if got := fedi.Challenge("dBjftJeZ4CVP-mJ92K9qkY4-ci5a7GMNPTzYJ2E2GzM"); got != "QRIkrs9GbiUvBEkmDI7Y7hfY2vRdAQNYqjsK4E92QRs" {
		t.Fatalf("challenge %q", got)
	}
}

func TestSuggestName(t *testing.T) {
	for in, want := range map[string]string{
		"alice":                                  "alice",
		"Alice_99":                               "Alice_99",
		"al ice!":                                "alice",
		"a":                                      "a",
		"!!":                                     "",
		"superuser":                              "",
		"averyveryveryveryveryverylongusername1": "averyveryveryveryveryverylonguse",
	} {
		if got := fedi.SuggestName(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestAuthorizeURL(t *testing.T) {
	inst := fedi.Instance{Host: "social.example", Base: "https://social.example"}
	u, _ := url.Parse(fedi.AuthorizeURL(inst, "cid", "https://k.example/api/fedi/callback", "profile", "st", "ch"))
	q := u.Query()
	if u.Scheme != "https" || u.Host != "social.example" || u.Path != "/oauth/authorize" ||
		q.Get("client_id") != "cid" || q.Get("response_type") != "code" || q.Get("scope") != "profile" ||
		q.Get("state") != "st" || q.Get("code_challenge") != "ch" || q.Get("code_challenge_method") != "S256" ||
		q.Get("redirect_uri") != "https://k.example/api/fedi/callback" {
		t.Fatalf("url %s", u)
	}
}

// testClient talks to a fake instance on httptest's TLS server (loopback).
func testClient(srv *httptest.Server) *fedi.Client {
	return fedi.NewClient(func(netip.AddrPort) bool { return true }, srv.Client().Transport.(*http.Transport).TLSClientConfig, false)
}

func fakeInstance(t *testing.T, f *fedifake.Instance) (*httptest.Server, fedi.Instance) {
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return srv, fedi.Instance{Host: u.Host, Base: srv.URL}
}

func TestTheFlowAgainstAFakeInstance(t *testing.T) {
	ctx := context.Background()
	for _, old := range []bool{false, true} {
		f := &fedifake.Instance{NoProfileScope: old}
		srv, inst := fakeInstance(t, f)
		c := testClient(srv)
		redirect := "https://k.example/api/fedi/callback"
		app, err := c.RegisterApp(ctx, inst, "Kallisti", redirect, "https://k.example")
		if err != nil || app.ClientID == "" || app.ClientSecret == "" {
			t.Fatalf("register: %+v %v", app, err)
		}
		scope := fedi.Scope(app.Scopes, false)
		if want := map[bool]string{false: "profile", true: "read:accounts"}[old]; scope != want {
			t.Fatalf("old %v: scope %q", old, scope)
		}
		verifier := fedi.NewVerifier()
		callback, err := f.Approve(fedi.AuthorizeURL(inst, app.ClientID, redirect, scope, "st", fedi.Challenge(verifier)), "alice")
		if err != nil {
			t.Fatal(err)
		}
		cb, _ := url.Parse(callback)
		if cb.Query().Get("state") != "st" {
			t.Fatalf("callback %s", callback)
		}
		// A wrong verifier gets no token (PKCE).
		if _, err := c.Token(ctx, inst, app, redirect, cb.Query().Get("code"), fedi.NewVerifier()); err == nil {
			t.Fatal("token without the right verifier")
		}
		callback, _ = f.Approve(fedi.AuthorizeURL(inst, app.ClientID, redirect, scope, "st", fedi.Challenge(verifier)), "alice")
		cb, _ = url.Parse(callback)
		token, err := c.Token(ctx, inst, app, redirect, cb.Query().Get("code"), verifier)
		if err != nil {
			t.Fatal(err)
		}
		acc, err := c.VerifyCredentials(ctx, inst, token)
		if err != nil || acc.ID == "" || acc.Username != "alice" || acc.Handle(inst) != "@alice@"+inst.Host {
			t.Fatalf("verify: %+v %v", acc, err)
		}
	}
}

func TestOddAnswersAreRefused(t *testing.T) {
	ctx := context.Background()
	for name, h := range map[string]http.HandlerFunc{
		"not json": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) },
		"too big": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"id":"1","username":"alice","note":"` + strings.Repeat("a", 3<<20) + `"}`))
		},
		"no id":        func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"username":"alice"}`)) },
		"null id":      func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"id":null,"username":"alice"}`)) },
		"float id":     func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"id":1e3,"username":"alice"}`)) },
		"bool id":      func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"id":true,"username":"alice"}`)) },
		"bad username": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"id":"1","username":"a b<script>"}`)) },
		"error status": func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{"id":"1","username":"a"}`, 500) },
	} {
		srv := httptest.NewTLSServer(h)
		u, _ := url.Parse(srv.URL)
		if acc, err := testClient(srv).VerifyCredentials(ctx, fedi.Instance{Host: u.Host, Base: srv.URL}, "t"); err == nil {
			t.Errorf("%s: %+v", name, acc)
		}
		srv.Close()
	}
	// Pleroma puts the user's front-end settings into the account: large.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"9","username":"lain","pleroma":{"settings_store":{"pleroma-fe":"` + strings.Repeat("x", 500_000) + `"}}}`))
	}))
	u, _ := url.Parse(srv.URL)
	if acc, err := testClient(srv).VerifyCredentials(ctx, fedi.Instance{Host: u.Host, Base: srv.URL}, "t"); err != nil || acc.Username != "lain" {
		t.Errorf("a large Pleroma account: %+v %v", acc, err)
	}
	srv.Close()
	// Numeric ids (some servers) are fine.
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":17,"username":"bob"}`))
	}))
	defer srv.Close()
	u, _ = url.Parse(srv.URL)
	if acc, err := testClient(srv).VerifyCredentials(ctx, fedi.Instance{Host: u.Host, Base: srv.URL}, "t"); err != nil || acc.ID != "17" {
		t.Fatalf("numeric id: %+v %v", acc, err)
	}
}

func TestTheGuardRefusesLocalAddresses(t *testing.T) {
	f := &fedifake.Instance{}
	srv, inst := fakeInstance(t, f)
	// The production guard, with the test server's certificate trusted: the
	// loopback address is refused when dialed.
	c := fedi.NewClient(nil, srv.Client().Transport.(*http.Transport).TLSClientConfig, false)
	_, err := c.RegisterApp(context.Background(), inst, "Kallisti", "https://k.example/cb", "")
	if err == nil || !errors.Is(err, fedi.ErrUnreachable) {
		t.Fatalf("loopback: %v", err)
	}
	if f.Requests() != 0 {
		t.Fatal("the request reached the server")
	}
}

func TestRedirectsStayOnHTTPS(t *testing.T) {
	plain := httptest.NewServer(&fedifake.Instance{})
	defer plain.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	if _, err := testClient(srv).VerifyCredentials(context.Background(), fedi.Instance{Host: u.Host, Base: srv.URL}, "t"); err == nil {
		t.Fatal("followed a redirect to http")
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

func TestAvatar(t *testing.T) {
	ctx := context.Background()
	f := &fedifake.Instance{}
	f.SetAvatar(pngOf(t, 300, 200))
	srv, inst := fakeInstance(t, f)
	c := testClient(srv)
	app, _ := c.RegisterApp(ctx, inst, "Kallisti", "https://k.example/cb", "")
	verifier := fedi.NewVerifier()
	callback, _ := f.Approve(fedi.AuthorizeURL(inst, app.ClientID, "https://k.example/cb", "profile", "st", fedi.Challenge(verifier)), "alice")
	cb, _ := url.Parse(callback)
	token, _ := c.Token(ctx, inst, app, "https://k.example/cb", cb.Query().Get("code"), verifier)
	acc, err := c.VerifyCredentials(ctx, inst, token)
	if err != nil || acc.Avatar != inst.Base+"/media/avatar_static" {
		t.Fatalf("verify: %+v %v", acc, err)
	}
	jpg, err := c.Avatar(ctx, acc.Avatar)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(jpg)); err != nil || format != "jpeg" || cfg.Width != 128 || cfg.Height != 128 {
		t.Fatalf("avatar: %v %s %v", cfg, format, err)
	}
	// Not a picture, too large to download, too many pixels.
	for name, data := range map[string][]byte{
		"broken":    []byte("<html>"),
		"too large": append(pngOf(t, 10, 10), make([]byte, 3<<20)...),
		"oversized": pngOf(t, 9000, 1),
	} {
		f.SetAvatar(data)
		if _, err := c.Avatar(ctx, acc.Avatar); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestAvatarURL(t *testing.T) {
	ctx := context.Background()
	for body, want := range map[string]string{
		`"avatar":"https://a.example/a.gif","avatar_static":"https://a.example/a.png"`: "https://a.example/a.png",
		`"avatar":"https://a.example/a.gif"`:                                           "https://a.example/a.gif",
		`"avatar":"http://a.example/a.png"`:                                            "",
		`"avatar":"javascript:alert(1)"`:                                               "",
		`"avatar":"https://m.example/avatars/original/missing.png"`:                    "", // Mastodon's default
		`"avatar":"https://p.example/images/avi.png"`:                                  "", // Pleroma's default
		`"avatar":""`: "",
	} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"id":"1","username":"bob",` + body + `}`))
		}))
		u, _ := url.Parse(srv.URL)
		acc, err := testClient(srv).VerifyCredentials(ctx, fedi.Instance{Host: u.Host, Base: srv.URL}, "t")
		if err != nil || acc.Avatar != want {
			t.Errorf("%s: %q %v", body, acc.Avatar, err)
		}
		srv.Close()
	}
}

func TestKnowsProfile(t *testing.T) {
	for v, want := range map[string]bool{
		"4.3.0": true, "4.4.1": true, "5.0.0-beta.1": true, "4.3.0+glitch": true,
		"4.2.12": false, "3.5.3": false, "": false,
		"2.7.2 (compatible; Pleroma 2.6.0)": false, "2.7.2 (compatible; Akkoma 3.13.2)": false,
		"0.17.3 git-abc": false,
	} {
		if fedi.KnowsProfile(v) != want {
			t.Errorf("%q: %v", v, !want)
		}
	}
}

func TestSingleUse(t *testing.T) {
	inst := fedi.Instance{Base: "https://melonbread.xyz", Host: "melonbread.xyz"}
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	for id, want := range map[string]bool{
		b64("https://melonbread.xyz/auth/843a3372-0199-440c-83e3-e469666c3ceb"): true,
		b64("https://other.example/auth/843a3372"):                              false,
		b64("https://melonbread.xyz/auth/"):                                     false,
		b64("https://melonbread.xyz/oauth/x"):                                   false,
		"843a3372-0199-440c-83e3-e469666c3ceb":                                  false,
		"Xc9wS3cL0gQz3G2hY1dS0fFkA3jKqP7m8rNtVbWxYz4":                           false,
		"": false,
	} {
		if fedi.SingleUse(inst, id) != want {
			t.Errorf("%q: %v", id, !want)
		}
	}
}
