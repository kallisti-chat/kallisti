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
	"strings"
	"sync/atomic"
	"testing"

	"kallisti.chat/server/internal/fedi"
)

const nativeMetadata = `{"issuer":"BASE","authorization_endpoint":"BASE/oauth/authorize","token_endpoint":"BASE/oauth/token","scopes_supported":["read:account"],"response_types_supported":["code"],"grant_types_supported":["authorization_code"],"code_challenge_methods_supported":["S256"],"authorization_response_iss_parameter_supported":true}`

func nativeOAuth(base string) *fedi.MisskeyOAuth {
	return &fedi.MisskeyOAuth{Issuer: base, AuthorizationEndpoint: base + "/oauth/authorize", TokenEndpoint: base + "/oauth/token", AuthorizationResponseISSSupported: true}
}

func TestDiscoverMisskeyOAuth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
		fail   bool
	}{
		{"native", 200, nativeMetadata, true, false},
		{"issuer trailing slash", 200, strings.Replace(nativeMetadata, `"issuer":"BASE"`, `"issuer":"BASE/"`, 1), true, false},
		{"issuer binding unadvertised", 200, strings.Replace(nativeMetadata, `,"authorization_response_iss_parameter_supported":true`, "", 1), true, false},
		{"default grant", 200, strings.Replace(nativeMetadata, `,"grant_types_supported":["authorization_code"]`, "", 1), true, false},
		{"not found", 404, "not found", false, false},
		{"wrong method", 405, "not supported", false, false},
		{"not implemented", 501, "not supported", false, false},
		{"unavailable", 503, "unavailable", false, true},
		{"malformed", 200, "<html>", false, true},
		{"empty", 200, `{}`, false, true},
		{"null", 200, `null`, false, true},
		{"wrong type", 200, strings.Replace(nativeMetadata, `"S256"`, `true`, 1), false, true},
		{"oversized", 200, strings.Repeat("x", 3<<20), false, true},
		{"wrong issuer", 200, strings.Replace(nativeMetadata, `"issuer":"BASE"`, `"issuer":"https://other.example"`, 1), false, true},
		{"issuer path", 200, strings.Replace(nativeMetadata, `"issuer":"BASE"`, `"issuer":"BASE/other"`, 1), false, true},
		{"issuer query", 200, strings.Replace(nativeMetadata, `"issuer":"BASE"`, `"issuer":"BASE?issuer=other"`, 1), false, true},
		{"issuer fragment", 200, strings.Replace(nativeMetadata, `"issuer":"BASE"`, `"issuer":"BASE#other"`, 1), false, true},
		{"remote authorize", 200, strings.Replace(nativeMetadata, "BASE/oauth/authorize", "https://other.example/authorize", 1), false, true},
		{"remote token", 200, strings.Replace(nativeMetadata, "BASE/oauth/token", "https://other.example/token", 1), false, true},
		{"wrong port", 200, strings.Replace(nativeMetadata, "BASE/oauth/token", "https://127.0.0.1:1/token", 1), false, true},
		{"http downgrade", 200, strings.Replace(nativeMetadata, "BASE/oauth/token", "http://other.example/token", 1), false, true},
		{"relative endpoint", 200, strings.Replace(nativeMetadata, "BASE/oauth/token", "/oauth/token", 1), false, true},
		{"credentials", 200, strings.Replace(nativeMetadata, "BASE/oauth/token", "https://user@other.example/token", 1), false, true},
		{"endpoint fragment", 200, strings.Replace(nativeMetadata, "BASE/oauth/authorize", "BASE/oauth/authorize#bad", 1), false, true},
		{"empty fragment", 200, strings.Replace(nativeMetadata, "BASE/oauth/token", "BASE/oauth/token#", 1), false, true},
		{"backslash", 200, strings.Replace(nativeMetadata, "BASE/oauth/authorize", `BASE/\\other.example`, 1), false, true},
		{"wrong scope", 200, strings.Replace(nativeMetadata, "read:account", "read:accounts", 1), false, true},
		{"implicit flow", 200, strings.Replace(nativeMetadata, `["code"]`, `["token"]`, 1), false, true},
		{"plain PKCE", 200, strings.Replace(nativeMetadata, "S256", "plain", 1), false, true},
		{"wrong grant", 200, strings.Replace(nativeMetadata, "authorization_code", "client_credentials", 1), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/.well-known/oauth-authorization-server" || r.Header.Get("Accept") != "application/json" {
					t.Errorf("unexpected discovery request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, strings.ReplaceAll(tc.body, "BASE", "https://"+r.Host))
			}))
			defer srv.Close()
			got, err := testClient(srv).DiscoverMisskeyOAuth(context.Background(), fedi.Instance{Base: srv.URL})
			if (got != nil) != tc.want || (err != nil) != tc.fail {
				t.Fatalf("metadata %+v, err %v", got, err)
			}
			if got != nil && (got.TokenEndpoint != srv.URL+"/oauth/token" || got.AuthorizationResponseISSSupported != (tc.name != "issuer binding unadvertised")) {
				t.Fatalf("lost discovery metadata: %+v", got)
			}
		})
	}
}

func TestMisskeyOAuthAuthorizeURL(t *testing.T) {
	meta := nativeOAuth("https://misskey.example")
	meta.AuthorizationEndpoint += "?server=setting&scope=write:notes&state=untrusted"
	verifier := fedi.NewVerifier()
	clientID := "https://k.example/api/fedi/client"
	redirect := "https://k.example/api/fedi/callback?other=a+b"
	u, err := url.Parse(meta.AuthorizeURL(clientID, redirect, "state & value", fedi.Challenge(verifier)))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "misskey.example" || u.Path != "/oauth/authorize" || q.Get("server") != "setting" ||
		q.Get("client_id") != clientID || q.Get("redirect_uri") != redirect || q.Get("response_type") != "code" ||
		q.Get("scope") != "read:account" || q.Get("state") != "state & value" || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") != fedi.Challenge(verifier) || len(q) != 8 {
		t.Fatalf("authorization URL %s", u)
	}
}

func TestMisskeyOAuthAccount(t *testing.T) {
	const user = `{"id":"a1b2","username":"alice","host":null,"avatarUrl":"https://images.example/alice.png"}`
	const token = `{"access_token":"account-token","token_type":"Bearer","scope":"read:account"}`
	for _, tc := range []struct {
		name   string
		token  string
		user   string
		status int
		fail   bool
		verify bool
		revoke bool
	}{
		{"approved", token, user, 200, false, true, true},
		{"scope omitted", strings.Replace(token, `,"scope":"read:account"`, "", 1), user, 200, false, true, true},
		{"case insensitive bearer", strings.Replace(token, "Bearer", "bearer", 1), user, 200, false, true, true},
		{"missing token", `{}`, "", 200, true, false, false},
		{"empty token", strings.Replace(token, "account-token", " ", 1), "", 200, true, false, false},
		{"malformed token", "<html>", "", 200, true, false, false},
		{"oversized token", strings.Repeat("x", 3<<20), "", 200, true, false, false},
		{"missing type", strings.Replace(token, `,"token_type":"Bearer"`, "", 1), "", 200, true, false, true},
		{"wrong type", strings.Replace(token, "Bearer", "MAC", 1), "", 200, true, false, true},
		{"wrong scope", strings.Replace(token, "read:account", "write:notes", 1), "", 200, true, false, true},
		{"error with token", strings.Replace(token, `"scope":`, `"error":"invalid_grant","scope":`, 1), "", 200, true, false, true},
		{"token denied", token, `{"error":{"code":"PERMISSION_DENIED"}}`, 403, true, true, true},
		{"remote account", token, strings.Replace(user, `"host":null`, `"host":"elsewhere.example"`, 1), 200, true, true, true},
		{"empty host", token, strings.Replace(user, `"host":null`, `"host":""`, 1), 200, true, true, true},
		{"invalid id", token, strings.Replace(user, `"a1b2"`, `"../a1b2"`, 1), 200, true, true, true},
		{"invalid username", token, strings.Replace(user, `"alice"`, `"alice@elsewhere.example"`, 1), 200, true, true, true},
		{"missing account", token, `null`, 200, true, true, true},
		{"malformed account", token, "<html>", 200, true, true, true},
		{"oversized account", token, strings.Repeat("x", 3<<20), 200, true, true, true},
		{"unsafe avatar", token, strings.Replace(user, "https://images.example/alice.png", "javascript:alert(1)", 1), 200, false, true, true},
		{"canceled verification", token, user, 200, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			verifier := fedi.NewVerifier()
			challenge := fedi.Challenge(verifier)
			var exchanges, verifications, revocations atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("invalid JSON body: %v", err)
				}
				switch r.URL.Path {
				case "/oauth/token":
					exchanges.Add(1)
					if body["grant_type"] != "authorization_code" || body["client_id"] != "https://k.example/api/fedi/client" ||
						body["redirect_uri"] != "https://k.example/api/fedi/callback" || body["scope"] != "read:account" ||
						body["code"] != "code & value" || fedi.Challenge(body["code_verifier"]) != challenge || len(body) != 6 {
						t.Errorf("unexpected public-client PKCE exchange: %v", body)
					}
					fmt.Fprint(w, tc.token)
				case "/api/i":
					verifications.Add(1)
					if body["i"] != "account-token" || len(body) != 1 {
						t.Errorf("unexpected verification: %v", body)
					}
					if tc.name == "canceled verification" {
						cancel()
						<-r.Context().Done()
						return
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.user)
				case "/api/i/revoke-token":
					revocations.Add(1)
					if (verifications.Load() == 1) != tc.verify || body["i"] != "account-token" || body["token"] != "account-token" || len(body) != 2 {
						t.Errorf("invalid revocation or revoked before verification: %v", body)
					}
					if tc.name == "approved" {
						w.WriteHeader(http.StatusNoContent)
					} else {
						http.Error(w, "self-revocation unsupported", http.StatusForbidden)
					}
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			a, err := testClient(srv).MisskeyOAuthAccount(ctx, fedi.Instance{Base: srv.URL}, nativeOAuth(srv.URL),
				"https://k.example/api/fedi/client", "https://k.example/api/fedi/callback", "code & value", verifier)
			if (err != nil) != tc.fail || exchanges.Load() != 1 || (verifications.Load() == 1) != tc.verify || (revocations.Load() == 1) != tc.revoke {
				t.Fatalf("account %+v, err %v, requests %d/%d/%d", a, err, exchanges.Load(), verifications.Load(), revocations.Load())
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

func TestMisskeyOAuthGuardAndRedirects(t *testing.T) {
	var requests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{}`)
	}))
	defer target.Close()
	ctx := context.Background()
	inst := fedi.Instance{Base: target.URL}
	production := fedi.NewClient(nil, target.Client().Transport.(*http.Transport).TLSClientConfig, false)
	if _, err := production.DiscoverMisskeyOAuth(ctx, inst); !errors.Is(err, fedi.ErrUnreachable) {
		t.Fatalf("unguarded discovery: %v", err)
	}
	if _, err := production.MisskeyOAuthAccount(ctx, inst, nativeOAuth(target.URL), "client", "callback", "code", fedi.NewVerifier()); !errors.Is(err, fedi.ErrUnreachable) {
		t.Fatalf("unguarded exchange: %v", err)
	}
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		for _, path := range []string{"/.well-known/oauth-authorization-server", "/oauth/token", "/api/i", "/api/i/revoke-token"} {
			t.Run(fmt.Sprintf("%d%s", status, path), func(t *testing.T) {
				source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == path {
						http.Redirect(w, r, target.URL+r.URL.Path, status)
						return
					}
					switch r.URL.Path {
					case "/oauth/token":
						fmt.Fprint(w, `{"access_token":"account-token","token_type":"Bearer"}`)
					case "/api/i":
						fmt.Fprint(w, `{"id":"a1b2","username":"alice"}`)
					case "/api/i/revoke-token":
						w.WriteHeader(http.StatusNoContent)
					default:
						http.NotFound(w, r)
					}
				}))
				defer source.Close()
				client := testClient(source)
				inst := fedi.Instance{Base: source.URL}
				if strings.HasPrefix(path, "/.well-known/") {
					if _, err := client.DiscoverMisskeyOAuth(ctx, inst); err == nil {
						t.Fatal("discovery followed redirect")
					}
				} else {
					_, err := client.MisskeyOAuthAccount(ctx, inst, nativeOAuth(source.URL), "client", "callback", "code", fedi.NewVerifier())
					if (err == nil) != (path == "/api/i/revoke-token") {
						t.Fatalf("unexpected redirected exchange/verification result: %v", err)
					}
				}
			})
		}
	}
	for _, field := range []string{"issuer", "authorize", "token", "nil"} {
		meta := nativeOAuth(target.URL)
		switch field {
		case "issuer":
			meta.Issuer = "https://other.example"
		case "authorize":
			meta.AuthorizationEndpoint = "https://other.example/authorize"
		case "token":
			meta.TokenEndpoint = "https://other.example/token"
		case "nil":
			meta = nil
		}
		if _, err := testClient(target).MisskeyOAuthAccount(ctx, inst, meta, "client", "callback", "code", fedi.NewVerifier()); !errors.Is(err, fedi.ErrInstance) {
			t.Fatalf("unbound %s metadata: %v", field, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("guarded requests reached target: %d", requests.Load())
	}
}

func TestMisskeyOAuthDevelopmentHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			fmt.Fprint(w, strings.ReplaceAll(nativeMetadata, "BASE", "http://"+r.Host))
		case "/oauth/token":
			fmt.Fprint(w, `{"access_token":"account-token","token_type":"Bearer"}`)
		case "/api/i":
			fmt.Fprint(w, `{"id":"a1b2","username":"alice"}`)
		case "/api/i/revoke-token":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	for _, dev := range []bool{false, true} {
		client := fedi.NewClient(func(netip.AddrPort) bool { return true }, nil, dev)
		inst := fedi.Instance{Base: srv.URL}
		got, err := client.DiscoverMisskeyOAuth(context.Background(), inst)
		if (got != nil) != dev || (err == nil) != dev {
			t.Fatalf("dev %v discovery: got %+v, %v", dev, got, err)
		}
		_, err = client.MisskeyOAuthAccount(context.Background(), inst, nativeOAuth(srv.URL), "client", "callback", "code", fedi.NewVerifier())
		if (err == nil) != dev {
			t.Fatalf("dev %v exchange: %v", dev, err)
		}
	}
}

func TestMisskeyOAuthInvalidGrant(t *testing.T) {
	verifier := fedi.NewVerifier()
	challenge := fedi.Challenge(verifier)
	var exchanges atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Errorf("failed grant reached %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		exchanges.Add(1)
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("malformed token request")
		}
		if body["code"] == "valid-code" && fedi.Challenge(body["code_verifier"]) == challenge {
			t.Error("test unexpectedly sent a valid grant")
		}
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	}))
	defer srv.Close()
	client := testClient(srv)
	inst := fedi.Instance{Base: srv.URL}
	for _, grant := range []struct{ code, verifier string }{
		{"valid-code", fedi.NewVerifier()},
		{"wrong-code", verifier},
		{"", verifier},
		{"valid-code", ""},
	} {
		account, err := client.MisskeyOAuthAccount(context.Background(), inst, nativeOAuth(srv.URL),
			"https://k.example/api/fedi/client", "https://k.example/api/fedi/callback", grant.code, grant.verifier)
		if err == nil || account != (fedi.Account{}) {
			t.Fatalf("invalid grant returned %+v, %v", account, err)
		}
	}
	if exchanges.Load() != 2 {
		t.Fatalf("empty code/verifier reached exchange: %d requests", exchanges.Load())
	}
}
