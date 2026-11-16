package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"kallisti.chat/server/internal/store"
)

// apiTokenPrefix marks personal access tokens (see store/apitokens.go):
// they are no sessions, and sessions are no tokens.
const apiTokenPrefix = "kallisti_pat_"

// APIAccount is the account of a request's personal access token
// (Authorization: Bearer), and whether it may write.
func (a *Service) APIAccount(r *http.Request) (store.Account, bool, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "+apiTokenPrefix)
	if !ok || token == "" {
		return store.Account{}, false, false
	}
	acc, write, err := a.Store.AccountByAPIToken(tokenHash(apiTokenPrefix + token))
	if err != nil {
		return store.Account{}, false, false
	}
	return acc, write, true
}

// apiTokens: an account's tokens, made, listed and revoked with a session
// (never with a token). Not for guest accounts.
func (a *Service) apiTokens(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.AccountFor(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}
	if acc.Temporary {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "guest accounts have no access tokens"})
		return
	}
	var c struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Write bool   `json:"write"`
	}
	if r.Method != http.MethodGet && json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := a.Store.APITokens(acc.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot list the tokens"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tokens": list})
	case http.MethodPost:
		name := strings.TrimSpace(c.Name)
		if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the name needs 1 to 64 characters"})
			return
		}
		token := apiTokenPrefix + randomHex(32)
		t, err := a.Store.CreateAPIToken(acc.ID, name, tokenHash(token), c.Write, a.Now())
		if errors.Is(err, store.ErrTooManyTokens) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "too many tokens: revoke one first"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot make the token"})
			return
		}
		slog.Info("access token made", "account", acc.Name, "token", t.ID, "write", t.Write)
		writeJSON(w, http.StatusCreated, map[string]any{"token": token, "id": t.ID, "name": t.Name, "write": t.Write, "created": t.Created})
	case http.MethodDelete:
		if err := a.Store.DeleteAPIToken(acc.ID, c.ID); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such token"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
