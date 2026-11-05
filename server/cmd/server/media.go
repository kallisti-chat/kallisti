package main

import (
	"net/http"

	"kallisti.chat/server/internal/auth"
	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/sidecar"
	"kallisti.chat/server/internal/store"
)

func privateRoomMedia(accounts *auth.Service, hub *sidecar.Hub, voice *mumbled.Server) func(*http.Request, store.Upload) bool {
	return func(r *http.Request, upload store.Upload) bool {
		return hub.MayReadUpload(upload.Key, func(channel uint32, text bool) bool {
			hash := ""
			if acc, ok := accounts.AccountFor(r); ok {
				hash = accountCertHash(acc)
			}
			return voice.MayReadMedia(hash, channel, text)
		})
	}
}
