package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"kallisti.chat/server/internal/names"
	"kallisti.chat/server/internal/store"
)

// maxAccountServers: the most servers in an account's list.
const maxAccountServers = 100

// servers: the account's list of servers it joined (the rail; see
// store.AccountServers: each with the account's name there).
func (a *Service) servers(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.homeListAccount(w, r, "server list")
	if !ok {
		return
	}
	// account: the one the client means (a stale window after another
	// login in this browser must not change that one's list).
	var body struct {
		store.AccountServer
		Account string `json:"account"`
	}
	if r.Method != http.MethodGet && json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
		sendError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !meant(w, r, acc, body.Account) {
		return
	}
	var err error
	switch r.Method {
	case http.MethodGet:
		var list []store.AccountServer
		if list, err = a.Store.AccountServers(acc.ID); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"servers": list})
			return
		}
	case http.MethodPost:
		body.Name = strings.TrimSpace(body.Name)
		if !a.otherOrigin(body.Origin) || !validListName(body.Name) {
			sendError(w, http.StatusBadRequest, "not a server's address and name")
			return
		}
		if err = a.Store.AddAccountServer(acc.ID, body.AccountServer, maxAccountServers); errors.Is(err, store.ErrTooManyServers) {
			sendError(w, http.StatusConflict, "your list is full")
			return
		} else if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
	case http.MethodDelete:
		if err = a.Store.RemoveAccountServer(acc.ID, body.Origin); err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	sendError(w, http.StatusInternalServerError, "cannot change the server list")
}

// homeListAccount: the logged-in account, for a list it keeps on its home
// server (the rail's servers, friends): from our own site and the apps;
// guests have none (nothing of theirs survives on a shared device).
func (a *Service) homeListAccount(w http.ResponseWriter, r *http.Request, what string) (store.Account, bool) {
	if !a.ownSite(w, r, "the "+what+" is only on your home server's own site") {
		return store.Account{}, false
	}
	acc, ok := a.AccountFor(r)
	if !ok {
		sendError(w, http.StatusUnauthorized, "not logged in")
		return store.Account{}, false
	}
	if acc.Temporary {
		sendError(w, http.StatusForbidden, "set a password first: guest accounts keep no "+what)
		return store.Account{}, false
	}
	return acc, true
}

// meant: the request is for this account (account: the name the client
// means, "" for any; on GET from the query). A stale window after another
// login in this browser must not change that one's lists.
func meant(w http.ResponseWriter, r *http.Request, acc store.Account, account string) bool {
	if r.Method == http.MethodGet {
		account = r.URL.Query().Get("account")
	}
	if account != "" && names.Key(account) != names.Key(acc.Name) {
		sendError(w, http.StatusConflict, "another login is active here now")
		return false
	}
	return true
}

// otherOrigin: a canonical web origin of another server than this one.
func (a *Service) otherOrigin(origin string) bool {
	return canonicalOrigin(origin) && !slices.Contains(a.AllowedOrigins, origin)
}

// canonicalOrigin: a web origin as browsers write it (scheme and lower-case
// host, no default port, no path).
func canonicalOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || len(origin) > 300 {
		return false
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	defaultPort := map[string]string{"https": "443", "http": "80"}[u.Scheme]
	return origin == u.Scheme+"://"+strings.ToLower(u.Host) && u.Port() != defaultPort && !strings.HasSuffix(u.Hostname(), ".")
}

// validListName: the account's name on that server (its rules may differ
// from ours: loosely checked).
func validListName(name string) bool {
	return name != "" && utf8.RuneCountInString(name) <= names.MaxRemote && !strings.ContainsFunc(name, unicode.IsControl)
}
