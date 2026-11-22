package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"kallisti.chat/server/internal/store"
)

// maxFriends: the most friends in an account's list.
const maxFriends = 500

// friends: the account's friends list (see store.Friend: accounts on this
// server or others, by user id there), kept here for all its devices.
func (a *Service) friends(w http.ResponseWriter, r *http.Request) {
	acc, ok := a.homeListAccount(w, r, "friends list")
	if !ok {
		return
	}
	var body struct {
		Origin  string `json:"origin"`
		User    *int64 `json:"user"` // required: none is not user 0
		Name    string `json:"name"`
		Account string `json:"account"`
	}
	if r.Method != http.MethodGet && json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
		sendError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !meant(w, r, acc, body.Account) {
		return
	}
	valid := canonicalOrigin(body.Origin) && body.User != nil && *body.User >= 0 && *body.User <= 1<<32-1
	var err error
	switch r.Method {
	case http.MethodGet:
		var list []store.Friend
		if list, err = a.Store.Friends(acc.ID); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"friends": list})
			return
		}
	case http.MethodPost:
		body.Name = strings.TrimSpace(body.Name)
		if !valid || !validListName(body.Name) {
			sendError(w, http.StatusBadRequest, "not a server's address, a user id and a name")
			return
		}
		f := store.Friend{Origin: body.Origin, User: uint32(*body.User), Name: body.Name}
		if err = a.Store.AddFriend(acc.ID, f, maxFriends); errors.Is(err, store.ErrTooManyFriends) {
			sendError(w, http.StatusConflict, "your friends list is full")
			return
		} else if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
	case http.MethodDelete:
		if !valid {
			sendError(w, http.StatusBadRequest, "not a server's address and a user id")
			return
		}
		if err = a.Store.RemoveFriend(acc.ID, body.Origin, uint32(*body.User)); err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	sendError(w, http.StatusInternalServerError, "cannot change the friends list")
}
