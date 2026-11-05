package mumbled

// MayReadMedia is the experimental room-only HTTP media policy. A room
// readable by an anonymous user keeps public media; otherwise a current
// registered account is required. Effective ACLs include private parents
// and categories, not just the channel's own private flag.
//
// The certificate hash comes from a validated web account session, never
// a URL parameter, request header or display name. Like offlineAccess,
// this does not confer transient Mumble access-token or in-channel rights.
func (s *Server) MayReadMedia(hash string, channel uint32, text bool) bool {
	var reader *User
	if hash != "" && s.cfg.DB != nil {
		r, err := s.cfg.DB.UserByHash(hash)
		if err == nil && r != nil && !s.certBanned(r) {
			reader = &User{Registered: true, UserID: r.ID, Channel: NoChannel}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Channels[channel] == nil {
		return false
	}
	may := func(u *User) bool {
		if text {
			return s.state.Sees(u, channel)
		}
		return s.state.Sees(u, channel) && s.state.Has(u, channel, Enter)
	}
	// Classification and authorization share the same ACL snapshot.
	return may(&User{Channel: NoChannel}) || reader != nil && may(reader)
}
