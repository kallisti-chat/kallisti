package auth

import (
	"log/slog"
	"net/http"

	"kallisti.chat/server/internal/store"
)

// streamKey: GET tells whether the account has a stream key (for OBS, see
// the whip package), POST makes a new one and shows it this once, DELETE
// removes it. Keys are stored hashed, like sessions. Guest accounts get
// none.
func (a *Service) streamKey(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.account(w, r)
	if !ok {
		return
	}
	if !a.StreamKeys {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
		} else {
			sendError(w, http.StatusNotFound, "streaming from OBS is off on this server")
		}
		return
	}
	if acc.Temporary {
		sendError(w, http.StatusForbidden, "set a password first: guest accounts cannot stream from OBS")
		return
	}
	switch r.Method {
	case http.MethodGet:
		has, err := a.Store.HasStreamKey(acc.ID)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "cannot read the stream key")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": true, "exists": has})
	case http.MethodPost:
		if a.limited(w, r) {
			return
		}
		key := randomHex(32)
		if a.Store.SetStreamKey(acc.ID, tokenHash(key), a.Now()) != nil {
			sendError(w, http.StatusInternalServerError, "cannot make a stream key")
			return
		}
		a.streamKeyChanged(acc.Name)
		writeJSON(w, http.StatusOK, map[string]string{"key": key})
	case http.MethodDelete:
		if a.Store.DeleteStreamKey(acc.ID) != nil {
			sendError(w, http.StatusInternalServerError, "cannot remove the stream key")
			return
		}
		a.streamKeyChanged(acc.Name)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// endStreamKey removes an account's stream key with its sessions (a new
// password, "log out everywhere"): it is a credential like them.
func (a *Service) endStreamKey(acc store.Account) {
	if err := a.Store.DeleteStreamKey(acc.ID); err != nil {
		slog.Error("removing a stream key", "account", acc.Name, "err", err)
	}
	a.streamKeyChanged(acc.Name)
}

func (a *Service) streamKeyChanged(name string) {
	if a.StreamKeyChanged != nil {
		a.StreamKeyChanged(name)
	}
}

// StreamKeyAccount is the account of a stream key (not a guest account).
func (a *Service) StreamKeyAccount(key string) (store.Account, bool) {
	if key == "" {
		return store.Account{}, false
	}
	acc, err := a.Store.AccountByStreamKey(tokenHash(key))
	return acc, err == nil && !acc.Temporary
}
