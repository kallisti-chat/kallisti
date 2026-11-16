package mumbled

import (
	"errors"
	"html"
	"log/slog"

	"kallisti.chat/server/internal/msgrules"
)

// Message rules (Kallisti, not Mumble): blocked words and link domains,
// and limits for new accounts and guests (see msgrules), saved and changed
// by admins. They apply to Mumble clients' texts here and to web clients'
// messages through CheckMessage. Staff are exempt, but not from word
// replacements (for fun, not moderation).

// MessageRules are the rules (admins only: the lists are for them).
func (s *Server) MessageRules(actor uint32) (msgrules.Rules, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.state.Users[actor]; u == nil || !s.isAdmin(u) {
		return msgrules.Rules{}, errors.New("only admins see the message rules")
	}
	return s.rules, nil
}

// SetMessageRules checks, saves and returns new rules (admins only).
func (s *Server) SetMessageRules(actor uint32, r msgrules.Rules) (msgrules.Rules, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	u := s.state.Users[actor]
	admin := u != nil && s.isAdmin(u)
	s.mu.Unlock()
	if !admin {
		return msgrules.Rules{}, errors.New("only admins change the message rules")
	}
	r, err := msgrules.Normalize(r)
	if err != nil {
		return msgrules.Rules{}, err
	}
	if s.cfg.DB != nil {
		if err := s.cfg.DB.SetMessageRules(r); err != nil {
			return msgrules.Rules{}, err
		}
	}
	s.mu.Lock()
	s.rules = r
	s.mu.Unlock()
	return r, nil
}

// CheckMessage gives the text to post, with the word replacements, or why
// the rules refuse a message of a session. An accepted new message of a
// limited user starts its wait.
func (s *Server) CheckMessage(session uint32, m msgrules.Message) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(session)
	if u == nil {
		return "", errors.New("not connected")
	}
	return s.checkMessageLocked(u, m)
}

func (s *Server) checkMessageLocked(u *User, m msgrules.Message) (string, error) {
	m.Text = msgrules.Replace(s.rules, m.Text, s.pick)
	now, key := s.now(), timeoutKeyOf(u)
	a := msgrules.Author{Staff: s.state.isStaff(u), Guest: !u.Registered || u.GuestAccount, Since: u.Since}
	err := msgrules.Check(s.rules, a, m, now, s.lastPost[key])
	switch {
	case errors.Is(err, msgrules.ErrWord) || errors.Is(err, msgrules.ErrDomain):
		// For moderators: there is no channel for such notes.
		slog.Info("message refused", "user", u.Name, "registered", u.Registered, "reason", err)
	case err == nil && m.Post && msgrules.Limited(s.rules, a, now):
		for k, t := range s.lastPost {
			if now.Sub(t) >= msgrules.Interval {
				delete(s.lastPost, k)
			}
		}
		s.lastPost[key] = now
	}
	return m.Text, err
}

// mumbleMessage is a Mumble client's text for the rules: the plain text
// that goes out (tags, and so link targets, do not).
func mumbleMessage(plain string, images bool) msgrules.Message {
	return msgrules.Message{Text: html.UnescapeString(plain), Upload: images, Post: true}
}
