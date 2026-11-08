package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"kallisti.chat/server/internal/fedi/fedifake"
	"kallisti.chat/server/internal/store"
)

func TestFediHandoffRevocation(t *testing.T) {
	for _, consent := range []bool{false, true} {
		for _, action := range []string{"valid", "unlink", "unlink-relink", "password", "logout-all", "wrong-password", "logout"} {
			name := action
			if consent {
				name += "/consent"
			}
			t.Run(name, func(t *testing.T) {
				f := &fedifake.Instance{}
				a, srv, inst := fediSetup(t, f)
				owner := newBrowser(t, srv)
				if resp, v := owner.do("POST", "/api/accounts", map[string]string{"name": "alice", "password": "old password"}); resp.StatusCode != http.StatusCreated {
					t.Fatalf("create: %d %v", resp.StatusCode, v)
				}
				wantRedirect(t, owner.fediFlow(f, map[string]any{"instance": inst, "link": true, "password": "old password"}, "Alice"), home+"?fedi=linked")

				popup := newBrowser(t, srv)
				origin := inst
				if consent {
					origin = "https://home.example"
				}
				start := map[string]any{"instance": inst, "handoff": true, "to": origin}
				wantRedirect(t, popup.fediFlow(f, start, "Alice"), home+"?fedi=connect")
				body := map[string]any{}
				if consent {
					resp, v := popup.do("POST", "/api/fedi/handoff", body)
					if resp.StatusCode != http.StatusOK || v["ask"] == nil || v["token"] != nil {
						t.Fatalf("consent: %d %v", resp.StatusCode, v)
					}
					body["allow"] = v["ask"]
				}

				if action != "valid" {
					path, payload := "/api/"+action, map[string]string{}
					if action == "unlink" || action == "unlink-relink" {
						path = "/api/fedi/unlink"
					}
					want := http.StatusNoContent
					if action == "password" || action == "wrong-password" {
						path = "/api/password"
						payload = map[string]string{"old": "old password", "new": "new password"}
						if action == "wrong-password" {
							payload["old"], want = "wrong password", http.StatusUnauthorized
						}
					}
					if resp, v := owner.do("POST", path, payload); resp.StatusCode != want {
						t.Fatalf("%s: %d %v", action, resp.StatusCode, v)
					}
					if action == "unlink-relink" {
						wantRedirect(t, owner.fediFlow(f, map[string]any{"instance": inst, "link": true, "password": "old password"}, "Alice"), home+"?fedi=linked")
					}
				}

				resp, v := popup.do("POST", "/api/fedi/handoff", body)
				if action == "valid" || action == "wrong-password" || action == "logout" {
					token, ok := v["token"].(string)
					if resp.StatusCode != http.StatusOK || !ok || token == "" || v["name"] != "alice" || v["origin"] != origin {
						t.Fatalf("valid handoff: %d %v", resp.StatusCode, v)
					}
					acc, err := a.Store.AccountBySession(tokenHash(token), a.Now())
					if err != nil || acc.Name != "alice" {
						t.Fatalf("redeemed session: %v %v", acc, err)
					}
				} else if resp.StatusCode != http.StatusNotFound || v["token"] != nil {
					t.Fatalf("handoff after %s: %d %v; want 404 without a token", action, resp.StatusCode, v)
				}
				if resp, v := popup.do("POST", "/api/fedi/handoff", body); resp.StatusCode != http.StatusNotFound || v["token"] != nil {
					t.Fatalf("second redemption: %d %v", resp.StatusCode, v)
				}
			})
		}
	}
}

// A local linked account, without a remote HTTP flow, for timing and race tests.
func handoffSetup(t *testing.T, ask bool) (*Service, *httptest.Server, store.Account, *http.Cookie, *http.Cookie) {
	t.Helper()
	a, srv := setup(t)
	now := time.Now()
	a.Now = func() time.Time { return now }
	a.Fedi = &FediLogin{Public: srv.URL, Dev: true}
	acc, err := a.Store.CreateAccount(store.Account{Name: "alice", PasswordHash: "hash", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Cookie{Name: CookieName, Value: "owner"}
	if err := a.Store.CreateSession(tokenHash(owner.Value), acc.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	id := store.FediIdentity{Host: "home.example", RemoteID: "alice-id", Handle: "@alice@home.example"}
	if err := a.Store.LinkFedi(acc.ID, tokenHash(owner.Value), id, now); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if !a.handOff(w, httptest.NewRequest("GET", "/", nil), acc, id, "https://home.example", ask) {
		t.Fatal("create handoff")
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == handoffCookie {
			return a, srv, acc, owner, cookie
		}
	}
	t.Fatal("no handoff cookie")
	return nil, nil, store.Account{}, nil, nil
}

func TestFediHandoffExpiry(t *testing.T) {
	for _, ask := range []bool{false, true} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("consent=%v/expired=%v", ask, expired), func(t *testing.T) {
				a, srv, _, _, cookie := handoffSetup(t, ask)
				life := handoffLifetime
				if ask {
					life = handoffAskLifetime
				}
				now := a.Now().Add(life)
				if !expired {
					now = now.Add(-time.Nanosecond)
				}
				a.Now = func() time.Time { return now }
				body := map[string]any{}
				if ask && !expired {
					_, v := post(t, srv, "/api/fedi/handoff", body, cookie)
					body["allow"] = v["ask"]
				}
				resp, v := post(t, srv, "/api/fedi/handoff", body, cookie)
				if expired {
					if resp.StatusCode != http.StatusNotFound || v["token"] != nil {
						t.Fatalf("expired: %d %v", resp.StatusCode, v)
					}
				} else if resp.StatusCode != http.StatusOK || v["token"] == nil {
					t.Fatalf("before expiry: %d %v", resp.StatusCode, v)
				}
			})
		}
	}
}

func TestFediHandoffRechecksIdentity(t *testing.T) {
	a, srv, acc, owner, cookie := handoffSetup(t, false)
	// Even a store-level unlink, outside the HTTP invalidation path, fails closed.
	if err := a.Store.UnlinkFedi(acc.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.LinkFedi(acc.ID, tokenHash(owner.Value), store.FediIdentity{Host: "other.example", RemoteID: "other", Handle: "@alice@other.example"}, a.Now()); err != nil {
		t.Fatal(err)
	}
	if resp, v := post(t, srv, "/api/fedi/handoff", map[string]any{}, cookie); resp.StatusCode != http.StatusNotFound || v["token"] != nil {
		t.Fatalf("changed identity: %d %v", resp.StatusCode, v)
	}
}

func TestFediHandoffConcurrentLogoutAll(t *testing.T) {
	for range 10 {
		a, srv, _, owner, cookie := handoffSetup(t, false)
		start := make(chan struct{})
		var wg sync.WaitGroup
		tokens := make(chan string, 8)
		for range cap(tokens) {
			wg.Go(func() {
				<-start
				resp, v := post(t, srv, "/api/fedi/handoff", map[string]any{}, cookie)
				switch resp.StatusCode {
				case http.StatusOK:
					tokens <- v["token"].(string)
				case http.StatusNotFound:
				default:
					t.Errorf("redemption: %d %v", resp.StatusCode, v)
				}
			})
		}
		wg.Go(func() {
			<-start
			if resp, v := post(t, srv, "/api/logout-all", map[string]any{}, owner); resp.StatusCode != http.StatusNoContent {
				t.Errorf("logout-all: %d %v", resp.StatusCode, v)
			}
		})
		close(start)
		wg.Wait()
		close(tokens)
		if len(tokens) > 1 {
			t.Fatalf("one handoff created %d sessions", len(tokens))
		}
		for token := range tokens {
			if _, err := a.Store.AccountBySession(tokenHash(token), a.Now()); err == nil {
				t.Fatal("a redeemed session survived concurrent logout-all")
			}
		}
	}
}
