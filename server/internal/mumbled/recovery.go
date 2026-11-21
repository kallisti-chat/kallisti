package mumbled

import (
	"errors"

	"kallisti.chat/server/internal/names"
)

// MayRecover: the actor may make a recovery link (a new password) for the
// registered user, by the rule for deleting users (see userListMsg):
// admins only, and for an admin only SuperUser. The user's name, and claim:
// empty for a Kallisti account's own registration; for a registration
// without an account (imported from Murmur, or made in a Mumble app) its
// certificate, which the link's new account takes over.
func (s *Server) MayRecover(actor, userID uint32) (name, claim string, err error) {
	if s.cfg.DB == nil {
		return "", "", errors.New("no registered users")
	}
	r, err := s.cfg.DB.UserByID(userID)
	if err != nil || r == nil || r.ID == 0 {
		return "", "", errors.New("no such user")
	}
	switch {
	case s.isAccount(r):
	// An account that only shares the name is not this user's.
	case s.cfg.ReservedName != nil && s.cfg.ReservedName(names.Key(r.Name)):
		return "", "", errors.New("a Kallisti account has this user's name")
	case r.CertHash == "":
		return "", "", errors.New("this user has no certificate")
	default:
		claim = r.CertHash
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.state.Users[actor]
	if u == nil || !s.isAdmin(u) {
		return "", "", errors.New("only admins make recovery links")
	}
	if s.state.aclPermissions(s.registeredLocked(r), 0)&Write != 0 && !isSuperUser(u) {
		return "", "", errors.New("only SuperUser makes recovery links for admins")
	}
	return r.Name, claim, nil
}
