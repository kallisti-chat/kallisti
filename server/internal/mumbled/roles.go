package mumbled

import (
	"errors"
	"maps"
	"slices"
)

// Roles, on top of Mumble's groups in root: admins (SuperUser and the
// "admin" group) may do everything; moderators (the "moderator" group) all
// but changing root's ACL and groups (where roles live) and the ban list,
// which need Write in root.

// moderatorACL gives moderators everything but Write (and Speak, so a
// lobby stays silent) in root and below, and Write below root.
func moderatorACL() []ACL {
	return []ACL{
		{ApplyHere: true, ApplySubs: true, Group: "moderator", Allow: AllPerms &^ (Write | Speak)},
		{ApplyHere: false, ApplySubs: true, Group: "moderator", Allow: Write},
	}
}

// isStaff: SuperUser, admins (also by a per-user ACL) and moderators.
// Only admins act on them (kick, ban, rename, unregister). By the ACLs
// alone: Permissions asks it (see readOnly).
func (s *State) isStaff(u *User) bool {
	return u.Registered && (isSuperUser(u) || s.aclPermissions(u, 0)&Write != 0 || s.groupMember(s.Channels[0], "moderator", u))
}

// Roles: registered user ids of admins and moderators.
func (s *Server) Roles() (admins, moderators []uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := s.state.Channels[0]
	ids := func(group string) []uint32 { return slices.Sorted(maps.Keys(s.state.members(root, group))) }
	return ids("admin"), ids("moderator")
}

// SetModerator makes a registered user a moderator, or not. Only admins
// (Write in root) may.
func (s *Server) SetModerator(actor, userID uint32, on bool) error {
	return s.setRole(actor, userID, "moderator", on, s.isAdmin, "only admins make moderators")
}

// SetAdmin makes a registered user an admin (only admins may), or not
// (only SuperUser: admins cannot remove each other).
func (s *Server) SetAdmin(actor, userID uint32, on bool) error {
	if on {
		return s.setRole(actor, userID, "admin", true, s.isAdmin, "only admins make admins")
	}
	return s.setRole(actor, userID, "admin", false, isSuperUser, "only SuperUser removes admins")
}

func (s *Server) isAdmin(u *User) bool { return s.state.Has(u, 0, Write) }

// setRole adds a registered user (not SuperUser) to a group in root, or
// removes them, if the actor may (checked under s.mu).
func (s *Server) setRole(actor, userID uint32, group string, on bool, may func(*User) bool, denied string) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if userID == 0 || !s.isRegistered(userID) {
		return errors.New("not a registered user")
	}
	s.mu.Lock()
	u := s.state.Users[actor]
	if u == nil || !may(u) {
		s.mu.Unlock()
		return errors.New(denied)
	}
	root := s.state.Channels[0]
	if root.Groups == nil {
		root.Groups = map[string]*Group{}
	}
	g := root.Groups[group]
	if g == nil {
		g = newGroup(group)
		root.Groups[group] = g
	}
	if on {
		g.Add[userID] = true
		delete(g.Remove, userID)
	} else {
		delete(g.Add, userID)
	}
	s.permissionsChangedLocked(root)
	save := cloneChannel(root)
	s.mu.Unlock()
	s.save(actor, []*Channel{save}, nil, 0)
	s.rolesChanged()
	return nil
}

// rolesChanged tells the Kallisti server (not under s.mu).
func (s *Server) rolesChanged() {
	if s.cfg.OnRolesChanged != nil {
		s.cfg.OnRolesChanged()
	}
}
