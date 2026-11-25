package mumbled

import "errors"

// Read-only text channels: as listen-only channels (listenonly.go), for
// TextMessage in a text channel's mirror. "all" may not write; admins,
// moderators and each listed user may. Every text post checks it
// (MayWrite), from the web, the agent API and Mumble clients alike.

// ReadOnlyChannel is a mirror where only staff and its Speakers write.
type ReadOnlyChannel = ListenOnlyChannel

var readOnlyACL = []ACL{
	// Also for subs (mirrors have none): here only, it would be exactly
	// categoryACL, and the mirror would count as a category (isCategory).
	{ApplyHere: true, ApplySubs: true, Group: "all", Deny: TextMessage},
	// The deny would take theirs away too (staff have no Write in mirrors).
	{ApplyHere: true, Group: "admin", Allow: TextMessage},
	{ApplyHere: true, Group: "moderator", Allow: TextMessage},
}

// textMirror: a text channel's mirror (not their parent nor a category).
// Under mu.
func (s *Server) textMirror(id uint32) bool {
	return id != s.textParent && s.state.mirror(id) && !s.isCategory(s.state.Channels[id])
}

// ReadOnlyChannels lists the read-only mirrors, with the names of the
// users who may still write.
func (s *Server) ReadOnlyChannels() []ReadOnlyChannel {
	return s.restricted(TextMessage, s.textMirror)
}

// SetReadOnlyChannel makes a text channel's mirror read-only for all but
// staff and the given registered users, or not (staff).
func (s *Server) SetReadOnlyChannel(actor, channel uint32, on bool, posters []uint32) error {
	return s.restrict(actor, channel, TextMessage, readOnlyACL, on, posters, func(u *User) error {
		switch {
		case !s.state.isStaff(u):
			return errors.New("only admins and moderators make channels read-only")
		case !s.textMirror(channel):
			return errors.New("only text channels can be read-only")
		}
		return nil
	})
}
