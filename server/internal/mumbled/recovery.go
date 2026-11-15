package mumbled

import "errors"

// MayRecover: the actor may make a recovery link (a new password) for the
// registered user, by the rule for deleting users (see userListMsg):
// admins only, and for an admin only SuperUser; only for a Kallisti
// account's own registration. The account's name.
func (s *Server) MayRecover(actor, userID uint32) (string, error) {
	if s.cfg.DB == nil {
		return "", errors.New("no registered users")
	}
	r, err := s.cfg.DB.UserByID(userID)
	if err != nil || r == nil {
		return "", errors.New("no such user")
	}
	// The account of this registration (name and certificate), not one
	// that only shares its name.
	if !s.isAccount(r) {
		return "", errors.New("this user has no Kallisti account")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.state.Users[actor]
	if u == nil || !s.isAdmin(u) {
		return "", errors.New("only admins make recovery links")
	}
	if s.state.aclPermissions(s.registeredLocked(r), 0)&Write != 0 && !isSuperUser(u) {
		return "", errors.New("only SuperUser makes recovery links for admins")
	}
	return r.Name, nil
}
