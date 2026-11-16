package mumbled

import (
	"errors"
	"html"
	"strings"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// MayRead: a user may see a channel's chat if it may enter the channel (or
// is in it). For the Kallisti server's stored voice channel chats.
func (s *Server) MayRead(session, channel uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(session)
	return u != nil && s.state.Channels[channel] != nil && (u.Channel == channel || s.state.Has(u, channel, Enter))
}

// UserChannel is the channel a session is in (text-only ones: none).
func (s *Server) UserChannel(session uint32) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.state.Users[session]; u != nil && !u.textOnly() {
		return u.Channel, true
	}
	return 0, false
}

// MaySpeak: a user may speak in a channel (and so stream there): Speak,
// and not muted or suppressed by others.
func (s *Server) MaySpeak(session, channel uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.state.Users[session]
	return u != nil && !u.Mute && !u.Suppress && !u.textOnly() && s.state.Channels[channel] != nil && s.state.Has(u, channel, Speak)
}

// MayWrite: a user may write to a channel's chat.
func (s *Server) MayWrite(session, channel uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(session)
	return u != nil && s.state.Channels[channel] != nil && s.state.Has(u, channel, TextMessage)
}

// SendChannelText delivers a plain text from a user to the others in a
// channel, as if the user had sent it (for messages written in the web
// client); from 0: from the server. Mumble clients read HTML, so it goes
// out escaped.
func (s *Server) SendChannelText(from, channel uint32, text string) {
	if mumbleHTML(text) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := &pb.TextMessage{Actor: proto.Uint32(from), ChannelId: []uint32{channel}, Message: proto.String(mumbleHTML(text))}
	if actor, ok := s.textActorLocked(from); ok {
		msg.Actor = proto.Uint32(actor)
	} else if u := s.state.Users[from]; u != nil {
		msg.Actor, msg.Message = nil, proto.String(mumbleHTML(u.Name+": "+text))
	} else if r := s.apiUsers[from]; r != nil { // the agent API: no session to name
		msg.Actor, msg.Message = nil, proto.String(mumbleHTML(r.Name+": "+text))
	} else if from == 0 {
		msg.Actor = nil
	}
	frame := mumble.MessageFrame(mumble.TypeTextMessage, msg)
	for _, u := range s.state.Users {
		if u.Channel == channel && u.Session != from {
			if c := s.clients[u.Session]; c != nil {
				c.send(frame, false)
			}
		}
	}
}

// mumbleHTML is plain text as Mumble's HTML: escaped, line breaks as tags.
func mumbleHTML(text string) string {
	return lineBreaks(html.EscapeString(strings.TrimSpace(text)))
}

// SendPrivateText sends plain text to a registered user's Mumble clients
// (not web clients) as a private message from a session; if the session is
// gone, the text starts with the sender's name. It reports whether any got
// it.
func (s *Server) SendPrivateText(from uint32, fromName string, toUser uint32, text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	msg := &pb.TextMessage{}
	s.mu.Lock()
	defer s.mu.Unlock()
	if actor, ok := s.textActorLocked(from); ok {
		msg.Actor = proto.Uint32(actor)
	} else {
		text = fromName + ": " + text
	}
	msg.Message = proto.String(mumbleHTML(text))
	delivered := false
	for _, u := range s.state.Users {
		if c := s.clients[u.Session]; u.Registered && u.UserID == toUser && c != nil && !c.web {
			msg.Session = []uint32{u.Session}
			c.send(mumble.MessageFrame(mumble.TypeTextMessage, msg), false)
			delivered = true
		}
	}
	return delivered
}

// MayManage: a user may manage the Kallisti server's text channels (Write
// in root: admins).
func (s *Server) MayManage(session uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(session)
	return u != nil && s.state.Has(u, 0, MakeChannel) // admins and moderators
}

// MessagesOf: whose recent messages an actor may delete at once (those who
// MayManage, as single messages; on staff as kicks), and how they are
// known: a connected user by session (session 0: a registered user by id,
// online or not), registered ones by user id, guests by session.
func (s *Server) MessagesOf(actor, session, user uint32) (registered bool, id uint32, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, u := s.state.Users[actor], &User{Registered: true, UserID: user}
	if session != 0 {
		u = s.state.Users[session]
	}
	switch {
	case a == nil || u == nil:
		return false, 0, errors.New("no such user")
	case !s.state.Has(a, 0, MakeChannel) || !s.mayActOn(a, u):
		return false, 0, errors.New("you may not delete this user's messages")
	case u.Registered:
		return true, u.UserID, nil
	}
	return false, u.Session, nil
}
