package mumbled

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"strings"
)

// Private channels: only their members, admins and moderators see them
// (see visibility.go) and may enter them. Plain ACL entries and a group in
// the channel, so Mumble clients see and edit the same thing: "all" lose
// Traverse and Enter; the "members" group, "moderator" and "admin" get
// them back. Subchannels inherit it. A private text channel is its
// mirror's ACL.
//
// A private category closes the channels in it, not itself (its entries
// apply to subchannels only): a private channel in it has its own members
// and the category's (its members group inherits, see inheritsMembers),
// and its own members reach it through the category.

const membersGroup = "members"

// privateBits are what non-members lose.
const privateBits = Traverse | Enter

// Member is a registered user in a private channel.
type Member = Speaker

// PrivateChannel is a channel only its members (and staff) see.
type PrivateChannel struct {
	Channel uint32
	Members []Member
	// Inherited: members by its category (not its own: the members dialog
	// edits those).
	Inherited []Member
}

// privateACL: here and below; subs: below only (a category). A
// category's entries count in each channel below it: its channels' own
// moderators pass there (Mumble asks Traverse at the category's level
// first, before their channel's entry).
func privateACL(subs bool) []ACL {
	acls := []ACL{{ApplyHere: !subs, ApplySubs: true, Group: "all", Deny: privateBits}}
	groups := []string{membersGroup, "moderator", "admin"}
	if subs {
		groups = append(groups, channelModGroup)
	}
	for _, g := range groups {
		acls = append(acls, ACL{ApplyHere: !subs, ApplySubs: true, Group: g, Allow: privateBits})
	}
	return acls
}

// isPrivateDeny: "all" may not traverse the channel (also by an entry
// from a Mumble client), or a private category's entry for the channels
// in it (only that one: an entry that hides subchannels is not privacy).
func isPrivateDeny(a ACL) bool {
	return !a.HasUser && a.Group == "all" && a.ApplyHere && a.Deny&Traverse != 0 || a == privateACL(true)[0]
}

func isPrivate(ch *Channel) bool { return slices.ContainsFunc(ch.ACLs, isPrivateDeny) }

// withoutPrivate drops the private entries and takes Traverse and Enter
// out of the denies for "all".
func withoutPrivate(acls []ACL) []ACL {
	allows := append(privateACL(false)[1:], privateACL(true)[1:]...)
	var out []ACL
	for _, a := range acls {
		if slices.Contains(allows, a) {
			continue
		}
		if isPrivateDeny(a) {
			a.Deny &^= privateBits
			if a.Allow == 0 && a.Deny == 0 {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// makePrivate sets a channel's private entries and members (known ids).
// Under mu.
func (s *Server) makePrivate(ch *Channel, members []uint32) {
	ch.ACLs = append(privateACL(s.isCategory(ch)), withoutPrivate(ch.ACLs)...)
	if ch.Groups == nil {
		ch.Groups = map[string]*Group{}
	}
	g := newGroup(membersGroup)
	g.Inherit = s.inheritsMembers(ch)
	for _, id := range members {
		g.Add[id] = true
	}
	ch.Groups[membersGroup] = g
}

// inheritsMembers: a channel's members group takes the members above it
// only in a channel directly in a category (the category's members); not
// elsewhere (voice channels, the category itself: a group SuperUser made
// higher up gives no access). Under mu.
func (s *Server) inheritsMembers(ch *Channel) bool {
	parent := s.state.Channels[ch.Parent]
	return ch.ID != 0 && parent != nil && s.isCategory(parent)
}

// InPrivate: the session's user is a member of the private channel (also
// by its category), not counting staff.
func (s *Server) InPrivate(session, channel uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ch := s.userLocked(session), s.state.Channels[channel]
	return u != nil && ch != nil && u.Registered && isPrivate(ch) && s.state.members(ch, membersGroup)[u.UserID]
}

// Private lists the private channels with their members' names.
func (s *Server) Private() []PrivateChannel {
	s.mu.Lock()
	var out []PrivateChannel
	ids := map[uint32]bool{}
	for _, id := range slices.Sorted(maps.Keys(s.state.Channels)) {
		ch := s.state.Channels[id]
		if id == 0 || !isPrivate(ch) {
			continue
		}
		p := PrivateChannel{Channel: id}
		own := map[uint32]bool{}
		if g := ch.Groups[membersGroup]; g != nil {
			own = g.Add
			for _, u := range slices.Sorted(maps.Keys(g.Add)) {
				p.Members = append(p.Members, Member{ID: u})
				ids[u] = true
			}
		}
		for _, u := range slices.Sorted(maps.Keys(s.state.members(ch, membersGroup))) {
			if !own[u] {
				p.Inherited = append(p.Inherited, Member{ID: u})
				ids[u] = true
			}
		}
		out = append(out, p)
	}
	s.mu.Unlock()
	names, err := s.userNames(slices.Collect(maps.Keys(ids)))
	named := func(ms []Member) []Member {
		if err == nil { // users who are gone; after an error all stay
			ms = slices.DeleteFunc(ms, func(m Member) bool { return names[m.ID] == "" })
		}
		for j := range ms {
			ms[j].Name = cmp.Or(names[ms[j].ID], "?")
		}
		return ms
	}
	for i := range out {
		out[i].Members, out[i].Inherited = named(out[i].Members), named(out[i].Inherited)
	}
	return out
}

// SetPrivate makes a channel private with these members (registered user
// ids), or public. Only admins and moderators may; the channel's
// moderators change the members of a private channel.
func (s *Server) SetPrivate(actor, channel uint32, on bool, members []uint32) error {
	if len(members) > maxGroupMembers {
		return errors.New("too many members")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	known := s.registeredIDs(members) // DB lookups outside the lock
	s.mu.Lock()
	ch, u := s.state.Channels[channel], s.state.Users[actor]
	switch {
	case ch == nil || u == nil:
		s.mu.Unlock()
		return errors.New("no such channel")
	case !s.state.isStaff(u) && !(on && isPrivate(ch) && s.state.isChannelMod(u, channel)):
		s.mu.Unlock()
		return errors.New("only admins and moderators make channels private")
	case channel == 0 || channel == s.textParent:
		s.mu.Unlock()
		return errors.New("this channel cannot be private")
	}
	if on {
		s.makePrivate(ch, slices.Collect(maps.Keys(known)))
	} else {
		ch.ACLs = withoutPrivate(ch.ACLs)
		delete(ch.Groups, membersGroup)
	}
	s.permissionsChangedLocked(ch)
	var save []*Channel
	if !ch.Temporary {
		save = []*Channel{cloneChannel(ch)}
	}
	s.mu.Unlock()
	s.save(save, nil, 0)
	s.privateChanged()
	return nil
}

// AddMember adds a registered user to a private channel's members (by an
// invite link: the caller checks it); added is false if it already was.
func (s *Server) AddMember(channel, user uint32) (added bool, err error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if !s.registeredIDs([]uint32{user})[user] {
		return false, errors.New("no such user")
	}
	s.mu.Lock()
	ch := s.state.Channels[channel]
	var g *Group
	if ch != nil && channel != 0 && isPrivate(ch) {
		g = ch.Groups[membersGroup]
	}
	switch {
	case g == nil:
		s.mu.Unlock()
		return false, errors.New("not a private channel")
	case g.Add[user]:
		s.mu.Unlock()
		return false, nil
	case len(g.Add) >= maxGroupMembers:
		s.mu.Unlock()
		return false, errors.New("too many members")
	}
	g.Add[user] = true
	delete(g.Remove, user)
	s.permissionsChangedLocked(ch)
	var save []*Channel
	if !ch.Temporary {
		save = []*Channel{cloneChannel(ch)}
	}
	s.mu.Unlock()
	s.save(save, nil, 0)
	s.privateChanged()
	return true, nil
}

// CreatePrivateChannel makes a private voice channel below a voice
// channel (or root), with no members yet: only staff see it. Admins and
// moderators may, and channel moderators in their channels (see
// modPermissions): they become its first member, to see it.
func (s *Server) CreatePrivateChannel(actor, parent uint32, name string) (uint32, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	u := s.state.Users[actor]
	switch {
	case u == nil || !s.state.isStaff(u) && s.state.modPermissions(u, parent)&MakeChannel == 0:
		s.mu.Unlock()
		return 0, errors.New("only admins and moderators make private channels")
	case s.state.Channels[parent] == nil || s.state.mirror(parent):
		s.mu.Unlock()
		return 0, errors.New("no such voice channel")
	}
	if err := s.state.modLimit(u, parent, 0); err != nil {
		s.mu.Unlock()
		return 0, err
	}
	ch, err := s.state.AddChannel(parent, strings.TrimSpace(name), false)
	if err != nil {
		s.mu.Unlock()
		return 0, err
	}
	if s.state.isStaff(u) {
		s.makePrivate(ch, nil)
		s.state.grantCreator(ch, u)
	} else {
		s.makePrivate(ch, []uint32{u.UserID})
	}
	s.showChannelLocked(ch)
	save, lastID := cloneChannel(ch), s.state.LastChannelID()
	s.mu.Unlock()
	s.save([]*Channel{save}, nil, lastID)
	s.privateChanged()
	return ch.ID, nil
}

// Sees: the session's user may see the channel (see visibility.go).
func (s *Server) Sees(session, channel uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(session)
	return u != nil && s.state.Channels[channel] != nil && s.state.Sees(u, channel)
}

// privateChanged tells the Kallisti server (not under s.mu).
func (s *Server) privateChanged() {
	if s.cfg.OnPrivateChanged != nil {
		s.cfg.OnPrivateChanged()
	}
}
