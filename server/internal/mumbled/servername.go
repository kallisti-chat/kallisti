package mumbled

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble/pb"
)

// ServerName is the root channel's name: an admin's (SetServerName), else
// Config.RootName.
func (s *Server) ServerName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Channels[0].Name
}

// SetServerName renames the server (admins only), saves the name and
// shows it to everyone as the root channel's. It gives the name as saved.
func (s *Server) SetServerName(actor uint32, name string) (string, error) {
	name = strings.TrimSpace(name)
	if err := validServerName(name); err != nil {
		return "", err
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	u := s.state.Users[actor]
	admin := u != nil && s.isAdmin(u)
	s.mu.Unlock()
	if !admin {
		return "", errors.New("only admins change this setting")
	}
	if s.cfg.DB != nil {
		if err := s.cfg.DB.SetServerName(name); err != nil {
			return "", err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Channels[0].Name = name
	s.sendChannelLocked(&pb.ChannelState{ChannelId: proto.Uint32(0), Name: proto.String(name)})
	return name, nil
}

// validServerName: freer than a channel's (the root is never looked up by
// name): any text on one line, up to a channel name's length.
func validServerName(name string) error {
	switch {
	case name == "":
		return errors.New("the name is empty")
	case utf8.RuneCountInString(name) > maxChannelName:
		return fmt.Errorf("the name is longer than %d characters", maxChannelName)
	case strings.ContainsFunc(name, unicode.IsControl):
		return errors.New("the name has control characters")
	}
	return nil
}

// IsAdmin: the session's user is an admin.
func (s *Server) IsAdmin(session uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.state.Users[session]
	return u != nil && s.isAdmin(u)
}
