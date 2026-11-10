package fedi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"kallisti.chat/server/internal/fedi"
)

func TestMiAuthURL(t *testing.T) {
	session := fedi.NewMiAuthSession()
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(session) || session == fedi.NewMiAuthSession() {
		t.Fatalf("session is not a fresh UUID v4: %q", session)
	}
	callback := "https://k.example/api/fedi/callback?state=a+b&other=value"
	inst := fedi.Instance{Host: "misskey.example", Base: "https://misskey.example"}
	u, err := url.Parse(fedi.MiAuthURL(inst, session, "Kallisti & friends", callback))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "https" || u.Host != inst.Host || u.Path != "/miauth/"+session ||
		q.Get("permission") != "read:account" || q.Get("callback") != callback || q.Get("name") != "Kallisti & friends" || len(q) != 3 {
		t.Fatalf("url %s", u)
	}
}

func TestIsMisskey(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
		fail   bool
	}{
		{"native", 200, `{"features":{"miauth":true}}`, true, false},
		{"disabled", 200, `{"features":{"miauth":false}}`, false, false},
		{"other API", 200, `{"version":"4.3.0"}`, false, false},
		{"not found", 404, "not found", false, false},
		{"wrong method", 405, "not supported", false, false},
		{"not implemented", 501, "not supported", false, false},
		{"unavailable", 503, "unavailable", false, true},
		{"malformed", 200, "<html>", false, true},
		{"wrong type", 200, `{"features":{"miauth":"true"}}`, false, true},
		{"oversized", 200, `{"padding":"` + strings.Repeat("x", 3<<20) + `"}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]bool
				if r.Method != http.MethodPost || r.URL.Path != "/api/meta" || r.Header.Get("Content-Type") != "application/json" ||
					json.NewDecoder(r.Body).Decode(&body) != nil || !body["detail"] {
					t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			got, err := testClient(srv).IsMisskey(context.Background(), fedi.Instance{Base: srv.URL})
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestMiAuthAccount(t *testing.T) {
	const user = `{"id":"a1b2","username":"alice","host":null,"avatarUrl":"https://images.example/alice.png"}`
	const check = `{"ok":true,"token":"account-token","user":` + user + `}`
	for _, tc := range []struct {
		name   string
		check  string
		user   string
		status int
		fail   bool
	}{
		{"approved", check, user, 200, false},
		{"denied", `{"ok":false}`, "", 200, true},
		{"missing approval", `{"token":"account-token","user":` + user + `}`, "", 200, true},
		{"missing token", `{"ok":true,"user":` + user + `}`, "", 200, true},
		{"missing user", `{"ok":true,"token":"account-token"}`, "", 200, true},
		{"malformed check", `<html>`, "", 200, true},
		{"oversized check", strings.Repeat("x", 3<<20), "", 200, true},
		{"remote check", strings.Replace(check, `"host":null`, `"host":"elsewhere.example"`, 1), "", 200, true},
		{"invalid check id", strings.Replace(check, `"a1b2"`, `"../a1b2"`, 1), "", 200, true},
		{"invalid check username", strings.Replace(check, `"alice"`, `"alice@elsewhere.example"`, 1), "", 200, true},
		{"token denied", check, `{"error":{"code":"PERMISSION_DENIED"}}`, 403, true},
		{"different id", check, strings.Replace(user, `"a1b2"`, `"other"`, 1), 200, true},
		{"different username", check, strings.Replace(user, `"alice"`, `"bob"`, 1), 200, true},
		{"remote account", check, strings.Replace(user, `"host":null`, `"host":"elsewhere.example"`, 1), 200, true},
		{"empty host", check, strings.Replace(user, `"host":null`, `"host":""`, 1), 200, true},
		{"missing account", check, `null`, 200, true},
		{"malformed account", check, `<html>`, 200, true},
		{"unsafe avatar", check, strings.Replace(user, "https://images.example/alice.png", "javascript:alert(1)", 1), 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := fedi.NewMiAuthSession()
			var checks, verifications atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("invalid JSON body: %v", err)
				}
				switch r.URL.Path {
				case "/api/miauth/" + session + "/check":
					checks.Add(1)
					if len(body) != 0 {
						t.Errorf("unexpected check body: %v", body)
					}
					fmt.Fprint(w, tc.check)
				case "/api/i":
					verifications.Add(1)
					if body["i"] != "account-token" || len(body) != 1 || tc.user == "" {
						t.Errorf("unexpected verification: %v", body)
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.user)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			a, err := testClient(srv).MiAuthAccount(context.Background(), fedi.Instance{Base: srv.URL}, session)
			if (err != nil) != tc.fail || checks.Load() != 1 || (tc.user != "") != (verifications.Load() == 1) {
				t.Fatalf("account %+v, err %v, requests %d/%d", a, err, checks.Load(), verifications.Load())
			}
			if !tc.fail {
				avatar := "https://images.example/alice.png"
				if tc.name == "unsafe avatar" {
					avatar = ""
				}
				if a.ID != "a1b2" || a.Username != "alice" || a.Avatar != avatar {
					t.Fatalf("account %+v", a)
				}
			} else if a != (fedi.Account{}) {
				t.Fatalf("failed verification returned account: %+v", a)
			}
		})
	}
}

func TestMiAuthGuardAndRedirects(t *testing.T) {
	var requests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"features":{"miauth":true}}`)
	}))
	defer target.Close()
	inst := fedi.Instance{Base: target.URL}
	production := fedi.NewClient(nil, target.Client().Transport.(*http.Transport).TLSClientConfig, false)
	if _, err := production.IsMisskey(context.Background(), inst); !errors.Is(err, fedi.ErrUnreachable) {
		t.Fatalf("unguarded metadata: %v", err)
	}
	if _, err := production.MiAuthAccount(context.Background(), inst, fedi.NewMiAuthSession()); !errors.Is(err, fedi.ErrUnreachable) {
		t.Fatalf("unguarded check: %v", err)
	}
	if _, err := testClient(target).MiAuthAccount(context.Background(), inst, "../other?session=stolen"); !errors.Is(err, fedi.ErrInstance) {
		t.Fatalf("invalid session: %v", err)
	}
	var approve atomic.Bool
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if approve.Load() && strings.HasPrefix(r.URL.Path, "/api/miauth/") {
			fmt.Fprint(w, `{"ok":true,"token":"account-token","user":{"id":"a1b2","username":"alice"}}`)
			return
		}
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	inst.Base = source.URL
	if _, err := testClient(source).IsMisskey(context.Background(), inst); err == nil {
		t.Fatal("metadata followed a redirect")
	}
	if _, err := testClient(source).MiAuthAccount(context.Background(), inst, fedi.NewMiAuthSession()); err == nil {
		t.Fatal("MiAuth session followed a redirect")
	}
	approve.Store(true)
	if _, err := testClient(source).MiAuthAccount(context.Background(), inst, fedi.NewMiAuthSession()); err == nil {
		t.Fatal("MiAuth token followed a redirect")
	}
	if requests.Load() != 0 {
		t.Fatalf("guarded requests reached target: %d", requests.Load())
	}
}

func TestMiAuthDevelopmentHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"features":{"miauth":true}}`)
	}))
	defer srv.Close()
	for _, dev := range []bool{false, true} {
		client := fedi.NewClient(func(netip.AddrPort) bool { return true }, nil, dev)
		got, err := client.IsMisskey(context.Background(), fedi.Instance{Base: srv.URL})
		if got != dev || (err == nil) != dev {
			t.Fatalf("dev %v: got %v, %v", dev, got, err)
		}
	}
}
