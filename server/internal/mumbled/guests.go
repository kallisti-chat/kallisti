package mumbled

import (
	"errors"

	"kallisti.chat/server/internal/names"
)

// Guests read only (Kallisti, not Mumble): a saved server setting, changed
// by admins. While it is on, guests (unregistered users and Kallisti guest
// accounts) have no TextMessage anywhere: they read and talk, but do not
// write. Staff are never guests.

// readOnly: the user is a guest that may not write.
func (s *State) readOnly(u *User) bool {
	return s.GuestsReadOnly && (!u.Registered || u.GuestAccount) && !s.isStaff(u)
}

// GuestsReadOnly reports the setting.
func (s *Server) GuestsReadOnly() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.GuestsReadOnly
}

// SetGuestsReadOnly changes the setting (admins only) and saves it.
func (s *Server) SetGuestsReadOnly(actor uint32, on bool) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	u := s.state.Users[actor]
	admin := u != nil && s.isAdmin(u)
	s.mu.Unlock()
	if !admin {
		return errors.New("only admins change this setting")
	}
	if s.cfg.DB != nil {
		if err := s.cfg.DB.SetGuestsReadOnly(on); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.state.GuestsReadOnly = on
	for _, u := range s.state.Users {
		s.flushPermissionsLocked(u) // Mumble clients show it
	}
	s.mu.Unlock()
	if s.cfg.OnGuestsReadOnlyChanged != nil {
		s.cfg.OnGuestsReadOnlyChanged()
	}
	return nil
}

// ReadOnly: the session's user is a guest that may not write now.
func (s *Server) ReadOnly(session uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(session)
	return u != nil && s.state.readOnly(u)
}

// AccountPermanent: a guest account got a password, so its users are no
// guests any more.
func (s *Server) AccountPermanent(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.state.Users {
		if u.GuestAccount && names.Key(u.Name) == names.Key(name) {
			u.GuestAccount = false
			s.flushPermissionsLocked(u)
		}
	}
}
