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
// user may.

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

func isSpeakerEntry(a ACL) bool {
	return a.HasUser && a.ApplyHere && a.Allow == Speak && a.Deny == 0
}

// isDenyEntry: "all" may not speak here (also an entry from a Mumble
// client, with more bits).
func isDenyEntry(a ACL) bool {
	return !a.HasUser && a.Group == "all" && a.ApplyHere && a.Deny&Speak != 0
}

// withoutListenOnly drops the speakers and takes Speak out of the denies.
func withoutListenOnly(acls []ACL) []ACL {
	var out []ACL
	for _, a := range acls {
		if isDenyEntry(a) {
			a.Deny &^= Speak
			if a.Allow == 0 && a.Deny == 0 {
				continue
			}
		}
		if !isSpeakerEntry(a) {
			out = append(out, a)
		}
	}
	return out
}

// listenOnlySpeakers: the channel's speakers, if "all" may not speak there
// (by an entry of its own, also one from a Mumble client).
func listenOnlySpeakers(ch *Channel) ([]uint32, bool) {
	on := false
	var ids []uint32
	for _, a := range ch.ACLs {
		if isDenyEntry(a) {
			on = true
		} else if isSpeakerEntry(a) {
			ids = append(ids, a.UserID)
		}
	}
	return ids, on
}

// ListenOnly lists the listen-only channels, with their speakers' names.
func (s *Server) ListenOnly() []ListenOnlyChannel {
	s.mu.Lock()
	var out []ListenOnlyChannel
	ids := map[uint32]bool{}
	for _, id := range slices.Sorted(maps.Keys(s.state.Channels)) {
		if speakers, on := listenOnlySpeakers(s.state.Channels[id]); on && id != 0 && !s.state.mirror(id) {
			c := ListenOnlyChannel{Channel: id}
			for _, u := range speakers {
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
	if len(speakers) > maxACLEntries {
		return errors.New("too many speakers")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	known, err := s.registeredIDs(speakers) // DB lookups outside the lock
	if err != nil {
		return errLookup
	}
	s.mu.Lock()
	ch, u := s.state.Channels[channel], s.state.Users[actor]
	switch {
	case ch == nil || u == nil:
		s.mu.Unlock()
		return errors.New("no such channel")
	case channel == 0 || s.state.mirror(channel):
		s.mu.Unlock()
		return errors.New("only voice channels can be listen-only")
	case s.state.aclPermissions(u, channel)&Write == 0: // not a channel moderator's (see modPermissions)
		s.mu.Unlock()
		return errors.New("only admins and moderators make channels listen-only")
	}
	ch.ACLs = withoutListenOnly(ch.ACLs)
	if on {
		ch.ACLs = append(ch.ACLs, listenOnlyDeny)
		for _, id := range slices.Compact(slices.Sorted(slices.Values(speakers))) {
			if known[id] {
				ch.ACLs = append(ch.ACLs, ACL{ApplyHere: true, HasUser: true, UserID: id, Allow: Speak})
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
		s.save(save, nil, 0)
	}
	s.listenOnlyChanged()
	return nil
}

// listenOnlyChanged tells the Kallisti server (not under s.mu).
func (s *Server) listenOnlyChanged() {
	if s.cfg.OnListenOnlyChanged != nil {
		s.cfg.OnListenOnlyChanged()
	}
}
