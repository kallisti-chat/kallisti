package main

import (
	"net/http"

	"kallisti.chat/server/internal/auth"
	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/sidecar"
	"kallisti.chat/server/internal/store"
)

// privateRoomMedia is the gate for a file link without a signature: the
// session's account, by its certificate (see sidecar.MayReadUpload).
func privateRoomMedia(accounts *auth.Service, hub *sidecar.Hub, voice *mumbled.Server) func(*http.Request, store.Upload) bool {
	return func(r *http.Request, u store.Upload) bool {
		return hub.MayReadUpload(u.Key, func() (uint32, bool) {
			acc, ok := accounts.AccountFor(r)
			if !ok {
				return 0, false
			}
			return voice.UserByCertHash(accountCertHash(acc))
		})
	}
}
