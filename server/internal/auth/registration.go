package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"kallisti.chat/server/internal/store"
)

func registrationError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, store.ErrRegistrationInvite) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "This server requires a valid account invitation. Ask an admin for a new link.", "code": "invite_required"})
		return true
	}
	return false
}

func (a *Service) mayRegister(w http.ResponseWriter, token string, temporary bool) bool {
	err := a.Store.CheckRegistration(token, temporary, a.Now())
	if err == nil {
		return true
	}
	if !registrationError(w, err) {
		slog.Error("check registration", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Cannot check registration right now. Please try again."})
	}
	return false
}
