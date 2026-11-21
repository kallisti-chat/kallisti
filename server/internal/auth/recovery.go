package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"unicode/utf8"

	"kallisti.chat/server/internal/certs"
	"kallisti.chat/server/internal/password"
	"kallisti.chat/server/internal/store"
)

// recover sets a new password with an admin's one-time recovery link (made
// in the sidecar, see sidecar/recovery.go): all the account's sessions and
// connections end, and this browser is logged in. A claim link (for a
// Mumble registration without an account) makes the account (see claim).
func (a *Service) recover(w http.ResponseWriter, r *http.Request) {
	if !a.allow(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var c struct {
		Token string `json:"token"`
		New   string `json:"new"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if n := utf8.RuneCountInString(c.New); n < 8 || n > 256 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the password needs 8 to 256 characters"})
		return
	}
	acc, err := a.Store.RecoveryAccount(c.Token, a.Now())
	if errors.Is(err, store.ErrNotFound) && a.MoveRegistration != nil {
		if link, err := a.Store.ClaimLinkOf(c.Token, a.Now()); err == nil {
			a.claim(w, r, c.Token, link, c.New)
			return
		}
	}
	var hash string
	if err == nil {
		hash, err = hashPassword(c.New)
		if err == nil {
			err = a.endLogins(acc, "", func() error { return a.Store.Recover(c.Token, acc.ID, hash, a.Now()) })
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this recovery link is used or expired: ask an admin for a new one"})
		return
	}
	if err != nil {
		slog.Error("recovery", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "please try again later"})
		return
	}
	slog.Info("account recovered", "account", acc.Name)
	acc.PasswordHash = hash
	token, ok := a.startSession(w, r, acc)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, withToken(map[string]any{"name": acc.Name}, token))
}

func hashPassword(pw string) (string, error) {
	defer password.Limit()()
	return password.Hash(pw)
}

// claim uses a claim link: a new account with the registration's name and
// the password, which takes over the registration (MoveRegistration, in the
// account's transaction: it keeps its id, groups and messages). Clients
// with the old certificate end: Mumble apps log in with the password now.
func (a *Service) claim(w http.ResponseWriter, r *http.Request, token string, link store.ClaimLink, pw string) {
	fail := func(err error) {
		slog.Error("claim link", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "please try again later"})
	}
	acc := store.Account{Name: link.Name}
	var err error
	if acc.PasswordHash, err = hashPassword(pw); err != nil {
		fail(err)
		return
	}
	if acc.CertPEM, acc.KeyPEM, err = certs.Generate(acc.Name); err != nil {
		fail(err)
		return
	}
	acc, err = a.Store.Claim(token, acc, a.Now(), a.MoveRegistration)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this recovery link is used or expired: ask an admin for a new one"})
		return
	case errors.Is(err, store.ErrNameTaken):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "an account has this name now: ask an admin"})
		return
	case err != nil:
		fail(err)
		return
	}
	if a.EndCertificateLogins != nil {
		a.EndCertificateLogins(link.CertHash)
	}
	slog.Info("registration claimed", "account", acc.Name, "user", link.User)
	if token, ok := a.startSession(w, r, acc); ok {
		writeJSON(w, http.StatusOK, withToken(map[string]any{"name": acc.Name}, token))
	}
}
