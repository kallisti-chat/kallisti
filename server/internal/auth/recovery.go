package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"unicode/utf8"

	"kallisti.chat/server/internal/password"
	"kallisti.chat/server/internal/store"
)

// recover sets a new password with an admin's one-time recovery link (made
// in the sidecar, see sidecar/recovery.go): all the account's sessions and
// connections end, and this browser is logged in.
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
	var hash string
	if err == nil {
		release := password.Limit()
		hash, err = password.Hash(c.New)
		release()
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
