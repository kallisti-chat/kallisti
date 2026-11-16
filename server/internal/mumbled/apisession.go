package mumbled

import "errors"

// API sessions: the agent API acts as a registered user without a Mumble
// connection. Their numbers have the top bit set (real sessions count up
// from 1); the chat checks find the user through userLocked.
const apiSessionBit = 1 << 31

// APISession is the API session of the registration with this certificate
// hash (a Kallisti account's), for the chat checks (Sees, MayRead,
// MayWrite, MayManage, CheckMessage).
func (s *Server) APISession(certHash string) (uint32, error) {
	if s.cfg.DB == nil || certHash == "" {
		return 0, errors.New("no registration")
	}
	r, err := s.cfg.DB.UserByHash(certHash)
	if err != nil || r == nil {
		return 0, errors.New("no registration: connect once with the account")
	}
	session := apiSessionBit | r.ID
	s.mu.Lock()
	s.apiUsers[session] = r
	s.mu.Unlock()
	return session, nil
}

// userLocked is a session's user: online, or an API session's registered
// user (see registeredLocked); nil: none.
func (s *Server) userLocked(session uint32) *User {
	if u := s.state.Users[session]; u != nil {
		return u
	}
	if r := s.apiUsers[session]; r != nil {
		return s.registeredLocked(r)
	}
	return nil
}
