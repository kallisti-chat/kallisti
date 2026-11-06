package mumbled

import (
	"log/slog"
	"net/netip"
)

// offlineAccess answers for a user who is not connected (push
// notifications, file links): a registered user by id, whose groups, roles
// and certificate count (access tokens do not), or with user nil a guest.
// It is in no channel. A banned user has none.
func (s *Server) offlineAccess(user *uint32, channel uint32, may func(u *User) bool) bool {
	u := &User{Channel: NoChannel}
	if user != nil {
		r := s.registered(*user)
		if r == nil || s.certBanned(r) {
			return false
		}
		u = &User{Registered: true, UserID: *user, Hash: r.CertHash, Channel: NoChannel}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Channels[channel] != nil && may(u)
}

func (s *Server) offlineSees(user *uint32, channel uint32) bool {
	return s.offlineAccess(user, channel, func(u *User) bool { return s.state.Sees(u, channel) })
}

func (s *Server) offlineMayRead(user *uint32, channel uint32) bool {
	return s.offlineAccess(user, channel, func(u *User) bool { return s.state.Has(u, channel, Enter) })
}

// UserSees and UserMayRead are Sees and MayRead for a registered user by
// id; GuestSees and GuestMayRead for a guest.
func (s *Server) UserSees(user, channel uint32) bool    { return s.offlineSees(&user, channel) }
func (s *Server) UserMayRead(user, channel uint32) bool { return s.offlineMayRead(&user, channel) }
func (s *Server) GuestSees(channel uint32) bool         { return s.offlineSees(nil, channel) }
func (s *Server) GuestMayRead(channel uint32) bool      { return s.offlineMayRead(nil, channel) }

// UserByCertHash is the registered user with this certificate.
func (s *Server) UserByCertHash(hash string) (uint32, bool) {
	if s.cfg.DB == nil {
		return 0, false
	}
	r, err := s.cfg.DB.UserByHash(hash)
	if err != nil {
		slog.Error("looking up a user", "err", err)
	}
	if r == nil {
		return 0, false
	}
	return r.ID, true
}

// UserBanned: a registered user's certificate is banned. An address ban
// cannot apply to a user who is not connected.
func (s *Server) UserBanned(user uint32) bool {
	r := s.registered(user)
	return r != nil && s.certBanned(r)
}

func (s *Server) registered(user uint32) *Registered {
	if s.cfg.DB == nil {
		return nil
	}
	r, err := s.cfg.DB.UserByID(user)
	if err != nil {
		slog.Error("looking up a user", "err", err)
	}
	return r
}

func (s *Server) certBanned(r *Registered) bool {
	return r.CertHash != "" && s.banned(netip.Addr{}, r.CertHash) != nil
}
