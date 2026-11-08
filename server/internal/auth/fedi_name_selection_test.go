package auth

import (
	"net/http"
	"strings"
	"testing"

	"kallisti.chat/server/internal/store"
)

func TestFediUnusableAutomaticNameCanChooseLocally(t *testing.T) {
	for _, remote := range []string{
		"superuser@home.example",
		strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + ".example",
	} {
		t.Run(remote, func(t *testing.T) {
			a, srv := setup(t)
			a.Fedi = &FediLogin{Public: srv.URL, Dev: true}
			id := store.FediIdentity{Host: "api.example", RemoteID: "42", Handle: "@" + remote}
			key := a.Fedi.signups.put(fediSignup{id: id, name: "suggested"}, a.Now(), a.Now().Add(signupLifetime))
			cookie := &http.Cookie{Name: signupCookie, Value: key}
			resp, v := post(t, srv, "/api/fedi/signup", map[string]string{}, cookie)
			if resp.StatusCode != http.StatusConflict || v["error"] == "" {
				t.Fatalf("automatic name must request name selection: %d %v", resp.StatusCode, v)
			}
			// The remote proof remains usable, but chosen local names are still validated.
			if resp, v = post(t, srv, "/api/fedi/signup", map[string]string{"name": "superuser"}, cookie); resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("invalid local choice: %d %v", resp.StatusCode, v)
			}
			resp, v = post(t, srv, "/api/fedi/signup", map[string]string{"name": "local-name"}, cookie)
			if resp.StatusCode != http.StatusCreated || v["name"] != "local-name" {
				t.Fatalf("valid local choice: %d %v", resp.StatusCode, v)
			}
			acc, linked, err := a.Store.AccountByFedi(id.Host, id.RemoteID)
			if err != nil || acc.Name != "local-name" || linked.Handle != id.Handle {
				t.Fatalf("wrong linked account: %v %v %v", acc, linked, err)
			}
			if resp, _ = post(t, srv, "/api/fedi/signup", map[string]string{"name": "another"}, cookie); resp.StatusCode != http.StatusNotFound {
				t.Fatalf("signup proof reused: %d", resp.StatusCode)
			}
		})
	}
}
