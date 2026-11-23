package mumbled

import (
	"errors"
	"maps"
	"slices"
)

// Channel moderators (Kallisti, on Mumble groups): moderators of one
// channel and its subchannels. A "channelmod" group in the channel holds
// them, and an ACL entry gives the group MuteDeafen and Move there (and
// Traverse and Enter, for private channels), so Mumble clients follow the
// same rules. Kallisti's rights (deletes, pins, topics, slow mode, private
// members and invite links, channel time-outs) ask the same group (see
// ModeratesIn). They never act on staff or on other moderators of the
// channel (see mayActOnIn). Admins and moderators appoint them.

const channelModGroup = "channelmod"

func channelModACL() ACL {
	return ACL{ApplyHere: true, ApplySubs: true, Group: channelModGroup, Allow: Traverse | Enter | MuteDeafen | Move}
}

// isChannelMod: the user is in the channel's "channelmod" group (inherited
// from channels above, as Mumble groups are) and sees the channel (a
// private subchannel stays hidden from a parent's moderators).
func (s *State) isChannelMod(u *User, channel uint32) bool {
	ch := s.Channels[channel]
	return ch != nil && s.groupMember(ch, channelModGroup, u) && s.Sees(u, channel)
}

// moderates: staff, or a moderator of the channel.
func (s *State) moderates(u *User, channel uint32) bool {
	return s.isStaff(u) || s.isChannelMod(u, channel)
}

// shields: the actor is no staff, and the user is staff or a moderator of
// the channel: the actor does not act on it, whatever the ACLs give.
func (s *State) shields(actor, u *User, channel uint32) bool {
	return !s.isStaff(actor) && (s.isStaff(u) || s.isChannelMod(u, channel))
}

// mayActOnIn is mayActOn in a channel (see shields).
func (s *Server) mayActOnIn(actor, u *User, channel uint32) bool {
	return s.mayActOn(actor, u) && !s.state.shields(actor, u, channel)
}

// ModeratesIn: the session's user is staff or a moderator of the channel.
func (s *Server) ModeratesIn(session, channel uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(session)
	return u != nil && s.state.moderates(u, channel)
}

// ChannelModerators are the registered users a channel's own group has
// (not those of channels above).
type ChannelModerators struct {
	Channel uint32
	Users   []uint32
}

// ChannelModerators lists the channels with moderators, by channel id.
func (s *Server) ChannelModerators() []ChannelModerators {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ChannelModerators
	for _, id := range slices.Sorted(maps.Keys(s.state.Channels)) {
		if g := s.state.Channels[id].Groups[channelModGroup]; id != 0 && g != nil && len(g.Add) > 0 {
			out = append(out, ChannelModerators{Channel: id, Users: slices.Sorted(maps.Keys(g.Add))})
		}
	}
	return out
}

// SetChannelModerator makes a registered user (not SuperUser) a moderator
// of a channel, or not. Only admins and moderators may.
func (s *Server) SetChannelModerator(actor, user, channel uint32, on bool) error {
	const denied = "only admins and moderators make channel moderators"
	if !s.ModeratesIn(actor, 0) { // first: others do not learn who is registered
		return errors.New(denied)
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if user == 0 || !s.isRegistered(user) {
		return errors.New("not a registered user")
	}
	s.mu.Lock()
	ch, u := s.state.Channels[channel], s.state.Users[actor]
	switch {
	case u == nil || !s.state.isStaff(u):
		s.mu.Unlock()
		return errors.New(denied)
	case ch == nil || channel == 0 || channel == s.textParent:
		s.mu.Unlock()
		return errors.New("this channel cannot have moderators")
	}
	if ch.Groups == nil {
		ch.Groups = map[string]*Group{}
	}
	g := ch.Groups[channelModGroup]
	if g == nil {
		g = newGroup(channelModGroup)
		ch.Groups[channelModGroup] = g
	}
	if on && len(g.Add) >= maxGroupMembers {
		s.mu.Unlock()
		return errors.New("too many moderators")
	}
	if on {
		g.Add[user] = true
		delete(g.Remove, user)
	} else {
		delete(g.Add, user)
	}
	// One entry while the group has moderators.
	ch.ACLs = slices.DeleteFunc(ch.ACLs, func(a ACL) bool { return a == channelModACL() })
	if len(g.Add) > 0 {
		ch.ACLs = append(ch.ACLs, channelModACL())
	} else if len(g.Remove) == 0 {
		delete(ch.Groups, channelModGroup)
	}
	s.permissionsChangedLocked(ch)
	var save []*Channel
	if !ch.Temporary {
		save = []*Channel{cloneChannel(ch)}
	}
	s.mu.Unlock()
	s.save(save, nil, 0)
	s.rolesChanged() // the Kallisti server resends private channels too
	return nil
}
