package main

import (
	"net/http"

	"kallisti.chat/server/internal/auth"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumbled"
)

// agentIdentity: an access token's account as an API session of its
// registration (see sidecar/agent.go, mumbled's APISession). Guest
// accounts have no tokens; an account that never connected has no
// registration yet.
func agentIdentity(accounts *auth.Service, srv *mumbled.Server) func(*http.Request) (mumble.Identity, bool, bool) {
	return func(r *http.Request) (mumble.Identity, bool, bool) {
		acc, write, ok := accounts.APIAccount(r)
		if !ok || acc.Temporary {
			return mumble.Identity{}, false, false
		}
		session, userID, err := srv.APISession(accountCertHash(acc))
		if err != nil {
			return mumble.Identity{}, false, false
		}
		return mumble.Identity{Session: session, Name: acc.Name, Registered: true, UserID: userID}, write, true
	}
}
