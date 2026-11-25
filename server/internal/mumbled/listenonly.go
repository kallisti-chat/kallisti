package mumbled

import (
	"cmp"
	"errors"
	"log/slog"
	"maps"
	"slices"
)

// Listen-only channels: plain ACL entries in the channel, so Mumble
// clients see and edit the same thing. "all" may not speak; each listed
// user may. Read-only text channels (readonly.go) are the same for
// TextMessage: p below is the permission that "all" lose and the listed
// users keep.

// Speaker is a registered user who may speak in a listen-only channel.
type Speaker struct {
	ID   uint32
	Name string
}

// ListenOnlyChannel is a channel where only its speakers may speak.
type ListenOnlyChannel struct {
	Channel  uint32
	Speakers []Speaker
}

var listenOnlyDeny = ACL{ApplyHere: true, Group: "all", Deny: Speak}

func isUserAllow(a ACL, p Perm) bool {
	return a.HasUser && a.ApplyHere && a.Allow == p && a.Deny == 0
}

// isAllDeny: "all" may not do p here (also an entry from a Mumble client,
// with more bits).
func isAllDeny(a ACL, p Perm) bool {
	return !a.HasUser && a.Group == "all" && a.ApplyHere && a.Deny&p != 0
}

// withoutRestriction drops the users' entries and the other allows, and
// takes p out of the denies.
func withoutRestriction(acls []ACL, p Perm, allows []ACL) []ACL {
	var out []ACL
	for _, a := range acls {
		if isAllDeny(a, p) {
			a.Deny &^= p
			if a.Allow == 0 && a.Deny == 0 {
				continue
			}
		}
		if !isUserAllow(a, p) && !slices.Contains(allows, a) {
			out = append(out, a)
		}
	}
	return out
}

// allowedUsers: the users who may do p in the channel, if "all" may not
// (by an entry of its own, also one from a Mumble client).
func allowedUsers(ch *Channel, p Perm) ([]uint32, bool) {
	on := false
	var ids []uint32
	for _, a := range ch.ACLs {
		if isAllDeny(a, p) {
			on = true
		} else if isUserAllow(a, p) {
			ids = append(ids, a.UserID)
		}
	}
	return ids, on
}

// ListenOnly lists the listen-only channels, with their speakers' names.
func (s *Server) ListenOnly() []ListenOnlyChannel {
	return s.restricted(Speak, func(id uint32) bool { return id != 0 && !s.state.mirror(id) })
}

// restricted lists the channels (of those kept, under mu) where "all" may
// not do p, with the names of the users who may.
func (s *Server) restricted(p Perm, keep func(id uint32) bool) []ListenOnlyChannel {
	s.mu.Lock()
	var out []ListenOnlyChannel
	ids := map[uint32]bool{}
	for _, id := range slices.Sorted(maps.Keys(s.state.Channels)) {
		if users, on := allowedUsers(s.state.Channels[id], p); on && keep(id) {
			c := ListenOnlyChannel{Channel: id}
			for _, u := range users {
				c.Speakers = append(c.Speakers, Speaker{ID: u})
				ids[u] = true
			}
			out = append(out, c)
		}
	}
	s.mu.Unlock()
	names, err := s.userNames(slices.Collect(maps.Keys(ids)))
	for i := range out {
		if err == nil { // users who are gone; after an error all stay
			out[i].Speakers = slices.DeleteFunc(out[i].Speakers, func(sp Speaker) bool { return names[sp.ID] == "" })
		}
		for j := range out[i].Speakers {
			out[i].Speakers[j].Name = cmp.Or(names[out[i].Speakers[j].ID], "?")
		}
	}
	return out
}

// UserNames are the names of registered users (unknown ids are left out).
func (s *Server) UserNames(ids []uint32) map[uint32]string {
	names, _ := s.userNames(ids)
	if names == nil {
		names = map[uint32]string{}
	}
	return names
}

func (s *Server) userNames(ids []uint32) (map[uint32]string, error) {
	if s.cfg.DB == nil || len(ids) == 0 {
		return nil, nil
	}
	names, err := s.cfg.DB.UserNames(ids)
	if err != nil {
		slog.Error("looking up users", "err", err)
	}
	return names, err
}

// SetListenOnly makes a voice channel listen-only for all but the given
// registered users, or not. It needs Write in the channel (admins and
// moderators).
func (s *Server) SetListenOnly(actor, channel uint32, on bool, speakers []uint32) error {
	return s.restrict(actor, channel, Speak, []ACL{listenOnlyDeny}, on, speakers, func(u *User) error {
		switch {
		case channel == 0 || s.state.mirror(channel):
			return errors.New("only voice channels can be listen-only")
		case s.state.aclPermissions(u, channel)&Write == 0: // not a channel moderator's (see modPermissions)
			return errors.New("only admins and moderators make channels listen-only")
		}
		return nil
	})
}

// restrict puts the entries (the deny for "all" first) and an allow of p
// for each of the users into the channel, or takes them out. refuse
// checks the actor and the channel, under mu.
func (s *Server) restrict(actor, channel uint32, p Perm, entries []ACL, on bool, users []uint32, refuse func(*User) error) error {
	if len(users) > maxACLEntries {
		return errors.New("too many users")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	known, err := s.registeredIDs(users) // DB lookups outside the lock
	if err != nil {
		return errLookup
	}
	s.mu.Lock()
	ch, u := s.state.Channels[channel], s.state.Users[actor]
	if ch == nil || u == nil {
		s.mu.Unlock()
		return errors.New("no such channel")
	}
	if err := refuse(u); err != nil {
		s.mu.Unlock()
		return err
	}
	ch.ACLs = withoutRestriction(ch.ACLs, p, entries[1:])
	if on {
		ch.ACLs = append(ch.ACLs, entries...)
		for _, id := range slices.Compact(slices.Sorted(slices.Values(users))) {
			if known[id] {
				ch.ACLs = append(ch.ACLs, ACL{ApplyHere: true, HasUser: true, UserID: id, Allow: p})
			}
		}
	}
	s.permissionsChangedLocked(ch)
	var save []*Channel
	if !ch.Temporary {
		save = []*Channel{cloneChannel(ch)}
	}
	s.mu.Unlock()
	if save != nil {
		s.save(actor, save, nil, 0)
	}
	s.listenOnlyChanged()
	return nil
}

// listenOnlyChanged tells the Kallisti server (not under s.mu): listen-only
// or read-only channels can have changed.
func (s *Server) listenOnlyChanged() {
	if s.cfg.OnListenOnlyChanged != nil {
		s.cfg.OnListenOnlyChanged()
	}
}
