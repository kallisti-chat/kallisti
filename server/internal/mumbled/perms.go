package mumbled

import (
	"cmp"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
	"kallisti.chat/server/internal/names"
)

// denyPerm is a PermissionDenied for a missing permission in a channel.
func (c *client) denyPerm(p Perm, channel uint32) {
	c.sendMsg(mumble.TypePermissionDenied, &pb.PermissionDenied{
		Type: pb.PermissionDenied_Permission.Enum(), Permission: proto.Uint32(uint32(p)),
		ChannelId: proto.Uint32(channel), Session: proto.Uint32(c.session),
	})
}

// enterRestricted: an ACL of the channel denies Enter (Murmur's
// isChannelEnterRestricted).
func enterRestricted(ch *Channel) bool {
	return slices.ContainsFunc(ch.ACLs, func(a ACL) bool { return a.Deny&Enter != 0 })
}

// channelStateFor is a channel's full state as one user sees it.
func (s *State) channelStateFor(ch *Channel, u *User) *pb.ChannelState {
	m := channelState(ch)
	m.CanEnter = proto.Bool(s.Has(u, ch.ID, Enter))
	m.IsEnterRestricted = proto.Bool(enterRestricted(ch))
	return m
}

// rootPerms is what ServerSync reports: the permissions in root. Murmur
// sends everything to SuperUser; here SuperUser gets its real permissions
// (no Speak), so clients show it the lobby too.
func (s *State) rootPerms(u *User) uint64 {
	return uint64(s.Permissions(u, 0))
}

// refreshLocked recomputes what a user may say and reports whether its
// suppress state changed.
func (s *Server) refreshLocked(u *User) bool {
	old := u.Suppress
	s.state.Refresh(u)
	return u.Suppress != old
}

// flushPermissionsLocked makes a client drop its cached permissions, with
// those of its channel, and of root (what clients show for the server,
// for example who may manage it).
func (s *Server) flushPermissionsLocked(u *User) {
	c := s.clients[u.Session]
	if c == nil {
		return
	}
	ch := u.Channel
	if u.textOnly() {
		ch = 0
	}
	c.sendMsg(mumble.TypePermissionQuery, &pb.PermissionQuery{
		ChannelId: proto.Uint32(ch), Permissions: proto.Uint32(uint32(s.state.Permissions(u, ch))),
		Flush: proto.Bool(true),
	})
	if ch != 0 {
		c.sendMsg(mumble.TypePermissionQuery, &pb.PermissionQuery{
			ChannelId: proto.Uint32(0), Permissions: proto.Uint32(uint32(s.state.Permissions(u, 0))),
		})
	}
}

// moveLocked moves a user into a channel and tells everyone, with the new
// suppress state: users who may not speak in a channel are suppressed.
func (s *Server) moveLocked(u *User, channel, actor uint32) {
	from := u.Channel
	removed := s.state.MoveUser(u.Session, channel)
	out := &pb.UserState{Session: proto.Uint32(u.Session), Actor: proto.Uint32(actor), ChannelId: proto.Uint32(u.Channel)}
	if s.refreshLocked(u) {
		out.Suppress = proto.Bool(u.Suppress)
	}
	s.sendMoveLocked(u, from, out)
	s.broadcastRemovedLocked(removed)
	s.broadcastUnlistenLocked(u, s.state.dropListener(u))
	s.flushPermissionsLocked(u) // @in, @out and @sub depend on the channel
	s.syncViewLocked(u)         // and so may what it sees
}

// broadcastUnlistenLocked tells everyone where a user stopped listening.
func (s *Server) broadcastUnlistenLocked(u *User, channels []uint32) {
	if len(channels) > 0 {
		s.sendUserLocked(u, &pb.UserState{Session: proto.Uint32(u.Session), ListeningChannelRemove: channels})
	}
}

// permissionsChangedLocked runs after ACLs, groups or registrations
// changed: clients drop their cached permissions, suppress states follow
// Speak, listeners without Listen stop, and (if a channel is given)
// everyone learns whether they may enter it.
func (s *Server) permissionsChangedLocked(changed *Channel) {
	s.state.Changed() // who may speak and whisper where
	s.syncViewsLocked()
	var below []uint32 // subchannels inherit the change
	if changed != nil {
		below = s.state.Subtree(changed.ID)
	}
	for _, u := range s.state.Users {
		s.flushPermissionsLocked(u)
		c := s.clients[u.Session]
		for _, id := range below {
			if ch := s.state.Channels[id]; c != nil && ch != nil && c.seen[id] {
				c.sendMsg(mumble.TypeChannelState, &pb.ChannelState{
					ChannelId: proto.Uint32(id), CanEnter: proto.Bool(s.state.Has(u, id, Enter)),
					IsEnterRestricted: proto.Bool(enterRestricted(ch)),
				})
			}
		}
	}
	dropped := s.state.DropListeners()
	for _, session := range slices.Sorted(maps.Keys(dropped)) {
		s.broadcastUnlistenLocked(s.state.Users[session], dropped[session])
	}
	for _, u := range s.state.Users {
		if s.refreshLocked(u) {
			s.sendUserLocked(u, &pb.UserState{Session: proto.Uint32(u.Session), Suppress: proto.Bool(u.Suppress)})
		}
	}
}

// keepWrite gives a user Write in a channel if the ACLs would not: for
// channel creators and ACL editors (as Murmur). Guests are named by their
// certificate.
func (s *State) keepWrite(ch *Channel, u *User) {
	if s.aclPermissions(u, ch.ID)&Write != 0 || (!u.Registered && u.Hash == "") {
		return
	}
	a := ACL{ApplyHere: true, Allow: Write | Traverse}
	if u.Registered {
		a.HasUser, a.UserID = true, u.UserID
	} else {
		a.Group = "$" + u.Hash
	}
	ch.ACLs = append(ch.ACLs, a)
}

// grantCreator makes a channel's creator its admin.
func (s *State) grantCreator(ch *Channel, u *User) {
	if u.Registered {
		if ch.Groups == nil {
			ch.Groups = map[string]*Group{}
		}
		g := newGroup("admin")
		g.Add[u.UserID] = true
		ch.Groups["admin"] = g
	}
	s.keepWrite(ch, u)
}

// mayEditACL: Write from the ACLs in the channel or in root (not a channel
// moderator's, see modPermissions). Needs Server.mu.
func (s *Server) mayEditACL(u *User, ch *Channel) bool {
	return s.state.aclPermissions(u, ch.ID)&Write != 0 || s.state.aclPermissions(u, 0)&Write != 0
}

// mayChangeACL: as mayEditACL, but root's ACL holds the roles, so only
// SuperUser changes it (admins could remove each other). Needs Server.mu.
func (s *Server) mayChangeACL(u *User, ch *Channel) bool {
	return s.mayEditACL(u, ch) && (ch.ID != 0 || isSuperUser(u))
}

func (s *Server) aclMsg(c *client, m *pb.ACL) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	ch := s.state.Channels[m.GetChannelId()]
	u := s.state.Users[c.session]
	if ch == nil || u == nil {
		s.mu.Unlock()
		return
	}
	if !s.mayEditACL(u, ch) {
		s.mu.Unlock()
		c.denyPerm(Write, ch.ID)
		return
	}
	if m.GetQuery() {
		reply, ids := s.state.aclReply(ch)
		s.mu.Unlock()
		c.sendMsg(mumble.TypeACL, reply)
		s.sendUserNames(c, ids)
		return
	}
	if !s.mayChangeACL(u, ch) {
		s.mu.Unlock()
		c.denyPerm(Write, ch.ID)
		return
	}
	s.mu.Unlock()
	known := s.registeredIDs(aclUserIDs(m)) // DB lookups outside the lock
	// Roles live in root's groups, channel moderators in channels'; runs
	// after the unlock.
	defer s.rolesChanged()
	defer s.listenOnlyChanged()
	defer s.privateChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, u = s.state.Channels[m.GetChannelId()], s.state.Users[c.session]
	if ch == nil || u == nil {
		return
	}
	if !s.mayChangeACL(u, ch) { // again: Write from @in, for example, can be gone now
		c.denyPerm(Write, ch.ID)
		return
	}
	ch.NoInheritACL = !m.GetInheritAcls()
	old := ch.Groups[channelModGroup]
	ch.ACLs, ch.Groups = aclsFromProto(m, known)
	if !s.state.isStaff(u) { // only staff appoint channel moderators
		delete(ch.Groups, channelModGroup)
		if old != nil {
			ch.Groups[channelModGroup] = old
		}
	}
	s.state.keepWrite(ch, u)
	s.permissionsChangedLocked(ch)
	if !ch.Temporary {
		save := cloneChannel(ch)
		s.mu.Unlock()
		s.save([]*Channel{save}, nil, 0)
		s.mu.Lock()
	}
}

// Limits for an edited ACL: Permissions runs over all of it, under the
// server lock.
const (
	maxACLEntries   = 100
	maxACLGroups    = 50
	maxGroupMembers = 200
	maxACLUsers     = 1000
)

// aclUserIDs are the user ids an ACL message names, once each.
func aclUserIDs(m *pb.ACL) []uint32 {
	ids := map[uint32]bool{}
	add := func(list ...uint32) {
		for _, id := range list {
			if len(ids) < maxACLUsers {
				ids[id] = true
			}
		}
	}
	for _, g := range m.Groups {
		add(g.Add...)
		add(g.Remove...)
	}
	for _, a := range m.Acls {
		if a.UserId != nil {
			add(a.GetUserId())
		}
	}
	return slices.Sorted(maps.Keys(ids))
}

// registeredIDs keeps the ids of registered users.
func (s *Server) registeredIDs(ids []uint32) map[uint32]bool {
	known := map[uint32]bool{}
	names, _ := s.userNames(ids)
	for id := range names {
		known[id] = true
	}
	return known
}

// aclsFromProto reads an edited ACL; entries for unknown users are dropped.
func aclsFromProto(m *pb.ACL, known map[uint32]bool) ([]ACL, map[string]*Group) {
	var acls []ACL
	// Like Murmur, "inherited" is not looked at: clients send only the
	// channel's own entries (and the field defaults to true).
	for _, a := range m.Acls[:min(len(m.Acls), maxACLEntries)] {
		e := ACL{ApplyHere: a.GetApplyHere(), ApplySubs: a.GetApplySubs(),
			Allow: Perm(a.GetGrant()) & AllPerms, Deny: Perm(a.GetDeny()) & AllPerms}
		if a.UserId != nil {
			if !known[a.GetUserId()] {
				continue
			}
			e.HasUser, e.UserID = true, a.GetUserId()
		} else {
			e.Group = a.GetGroup()
		}
		acls = append(acls, e)
	}
	groups := map[string]*Group{}
	for _, g := range m.Groups[:min(len(m.Groups), maxACLGroups)] {
		e := newGroup(g.GetName())
		e.Inherit, e.Inheritable = g.GetInherit(), g.GetInheritable()
		for _, id := range g.Add[:min(len(g.Add), maxGroupMembers)] {
			e.Add[id] = known[id]
		}
		for _, id := range g.Remove[:min(len(g.Remove), maxGroupMembers)] {
			e.Remove[id] = known[id]
		}
		maps.DeleteFunc(e.Add, func(_ uint32, ok bool) bool { return !ok })
		maps.DeleteFunc(e.Remove, func(_ uint32, ok bool) bool { return !ok })
		groups[e.Name] = e
	}
	return acls, groups
}

// aclReply is the ACL message for a channel's editor: its own entries and
// those handed down from parents, and its groups with inherited members
// (as Murmur's msgACL query). It returns the user ids named.
func (s *State) aclReply(ch *Channel) (*pb.ACL, []uint32) {
	ids := map[uint32]bool{}
	reply := &pb.ACL{ChannelId: proto.Uint32(ch.ID), InheritAcls: proto.Bool(!ch.NoInheritACL)}
	// Channels whose entries count: up from ch while channels inherit.
	var chain []*Channel
	for p := ch; p != nil; p = s.Channels[p.Parent] {
		chain = append(chain, p)
		if p.ID == 0 || p.NoInheritACL {
			break
		}
	}
	for _, p := range slices.Backward(chain) {
		for _, a := range p.ACLs {
			if p != ch && !a.ApplySubs {
				continue
			}
			e := &pb.ACL_ChanACL{
				Inherited: proto.Bool(p != ch), ApplyHere: proto.Bool(a.ApplyHere), ApplySubs: proto.Bool(a.ApplySubs),
				Grant: proto.Uint32(uint32(a.Allow)), Deny: proto.Uint32(uint32(a.Deny)),
			}
			if a.HasUser {
				e.UserId = proto.Uint32(a.UserID)
				ids[a.UserID] = true
			} else {
				e.Group = proto.String(a.Group)
			}
			reply.Acls = append(reply.Acls, e)
		}
	}
	for _, name := range s.groupNames(ch) {
		// Inherited defaults to true in Mumble.proto: always sent.
		e := &pb.ACL_ChanGroup{Name: proto.String(name), Inherit: proto.Bool(true), Inheritable: proto.Bool(true), Inherited: proto.Bool(false)}
		if g := ch.Groups[name]; g != nil {
			e.Inherit, e.Inheritable = proto.Bool(g.Inherit), proto.Bool(g.Inheritable)
			e.Add, e.Remove = slices.Sorted(maps.Keys(g.Add)), slices.Sorted(maps.Keys(g.Remove))
		}
		if ch.ID != 0 {
			if pch, pg := s.visibleGroup(s.Channels[ch.Parent], name); pg != nil {
				e.Inherited = proto.Bool(pg.Inheritable)
				e.InheritedMembers = slices.Sorted(maps.Keys(s.members(pch, name)))
			}
		}
		for _, id := range slices.Concat(e.Add, e.Remove, e.InheritedMembers) {
			ids[id] = true
		}
		reply.Groups = append(reply.Groups, e)
	}
	return reply, slices.Sorted(maps.Keys(ids))
}

// groupNames are the groups a channel's editor sees: its own, and
// inheritable ones from above (Group::groupNames).
func (s *State) groupNames(ch *Channel) []string {
	names := map[string]bool{}
	for _, p := range s.chain(ch.ID) {
		for name, g := range p.Groups {
			if p == ch || g.Inheritable {
				names[name] = true
			} else {
				delete(names, name)
			}
		}
	}
	return slices.Sorted(maps.Keys(names))
}

// visibleGroup is the group of that name as seen from a channel, and the
// channel that has it (Group::getGroup).
func (s *State) visibleGroup(from *Channel, name string) (*Channel, *Group) {
	for p := from; p != nil; p = s.Channels[p.Parent] {
		if g := p.Groups[name]; g != nil {
			if p == from || g.Inheritable {
				return p, g
			}
			return nil, nil
		}
		if p.ID == 0 {
			break
		}
	}
	return nil, nil
}

// sendUserNames tells the editor the names of the users in an ACL.
func (s *Server) sendUserNames(c *client, ids []uint32) {
	if s.cfg.DB == nil || len(ids) == 0 {
		return
	}
	byID, err := s.cfg.DB.UserNames(ids)
	if err != nil || len(byID) == 0 {
		return
	}
	out := &pb.QueryUsers{}
	for _, id := range slices.Sorted(maps.Keys(byID)) {
		out.Ids, out.Names = append(out.Ids, id), append(out.Names, byID[id])
	}
	c.sendMsg(mumble.TypeQueryUsers, out)
}

// userListMsg lists registered users, or renames (name given) and
// unregisters (no name) them. It needs Register in root; unregistering
// (deleting the user, with its web account) needs Write too (admins), and
// deleting an admin SuperUser (see SetAdmin).
func (s *Server) userListMsg(c *client, m *pb.UserList) {
	s.mu.Lock()
	u := s.state.Users[c.session]
	allowed := u != nil && s.state.Has(u, 0, Register)
	s.mu.Unlock()
	db := s.cfg.DB
	if !allowed {
		c.denyPerm(Register, 0)
		return
	}
	if db == nil {
		return
	}
	if len(m.Users) == 0 {
		users, err := db.Users()
		if err != nil {
			slog.Error("listing users", "err", err)
			return
		}
		out := &pb.UserList{}
		for _, r := range users {
			out.Users = append(out.Users, &pb.UserList_User{
				UserId: proto.Uint32(r.ID), Name: proto.String(r.Name), LastChannel: proto.Uint32(r.LastChannel),
			})
		}
		c.sendMsg(mumble.TypeUserList, out)
		return
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	for _, e := range m.Users {
		id := e.GetUserId()
		s.mu.Lock()
		admin := s.state.Has(u, 0, Write)
		target := &User{Registered: true, UserID: id}
		protected := s.state.isStaff(target) && !admin
		targetAdmin := s.state.aclPermissions(target, 0)&Write != 0
		s.mu.Unlock()
		if protected {
			c.denyPerm(Write, 0)
			continue
		}
		var refused string // a reason the user may see
		var err error
		account := s.accountName(id)
		if e.Name != nil && s.cfg.ReservedName != nil && s.cfg.ReservedName(names.Key(e.GetName())) {
			refused = "The name belongs to a Kallisti account."
		} else if e.Name != nil && account != "" {
			// Its name is the account's: renamed, the account could not join.
			refused = "This registration belongs to a Kallisti account: its name cannot change."
		} else if e.Name != nil {
			err = db.RenameUser(id, e.GetName())
		} else if !admin {
			refused = "Only admins can delete users."
		} else if targetAdmin && !isSuperUser(u) {
			refused = "Only SuperUser can delete admins."
		} else if account != "" && s.cfg.DeleteAccount != nil {
			if err = s.cfg.DeleteAccount(account); err == nil {
				err = db.UnregisterUser(id)
			}
		} else {
			err = db.UnregisterUser(id)
		}
		if refused != "" || err != nil {
			c.deny(deny(pb.PermissionDenied_Text, "%s", cmp.Or(refused, fmt.Sprintf("Cannot change user %d.", id))))
			continue
		}
		s.mu.Lock()
		s.registrationChangedLocked(id, e.Name, c.session)
		s.mu.Unlock()
		s.userChanged(id, e.GetName())
		if e.Name == nil {
			s.rolesChanged() // its group memberships are gone
		}
	}
}

// accountName: the name of the Kallisti web account whose registration
// this is ("" if none): its name and certificate.
func (s *Server) accountName(id uint32) string {
	if s.cfg.ReservedName == nil {
		return ""
	}
	r, err := s.cfg.DB.UserByID(id)
	if err != nil || r == nil || !s.cfg.ReservedName(names.Key(r.Name)) ||
		(s.cfg.WebAccountExists != nil && !s.cfg.WebAccountExists(r.Name, r.CertHash)) {
		return ""
	}
	return r.Name
}

// registrationChangedLocked updates an online user after a rename (name
// given) or after it was unregistered, and forgets its ACL entries.
func (s *Server) registrationChangedLocked(id uint32, name *string, actor uint32) {
	if keys := s.timeoutKeysLocked(timeoutKey{id: id}); name == nil && len(keys) > 0 {
		for _, k := range keys {
			s.setTimeoutLocked(k, time.Time{}, actor) // the database rows are gone too
		}
		go s.timeoutsChanged()
	}
	if name == nil {
		for _, ch := range s.state.Channels {
			ch.ACLs = slices.DeleteFunc(ch.ACLs, func(a ACL) bool { return a.HasUser && a.UserID == id })
			for _, g := range ch.Groups {
				delete(g.Add, id)
				delete(g.Remove, id)
			}
		}
	}
	for _, u := range s.state.Users {
		if !u.Registered || u.UserID != id {
			continue
		}
		out := &pb.UserState{Session: proto.Uint32(u.Session), Actor: proto.Uint32(actor)}
		if name != nil {
			u.Name = *name
			if c := s.clients[u.Session]; c != nil {
				c.name = *name
			}
			out.Name = name
		} else {
			u.Registered, u.UserID = false, 0
			out.UserId = proto.Uint32(0xffffffff) // -1: not registered, as Murmur sends it
		}
		s.sendUserLocked(u, out)
	}
	if name == nil {
		s.permissionsChangedLocked(nil)
	}
}

// Unregister removes the registration of a certificate (a temporary web
// account that is cleaned up), as an admin's removal does, unless its user
// is online. gone: no registration is left (also for unknown ones).
func (s *Server) Unregister(certHash string) (gone bool, err error) {
	db := s.cfg.DB
	if db == nil {
		return true, nil
	}
	s.regMu.Lock()
	defer s.regMu.Unlock()
	r, err := db.UserByHash(certHash)
	if err != nil || r == nil {
		return err == nil, err
	}
	s.mu.Lock()
	online := slices.ContainsFunc(slices.Collect(maps.Values(s.state.Users)), func(u *User) bool { return u.Registered && u.UserID == r.ID })
	s.mu.Unlock()
	if online {
		return false, nil
	}
	if err := db.UnregisterUser(r.ID); err != nil {
		return false, err
	}
	s.mu.Lock()
	s.registrationChangedLocked(r.ID, nil, 0)
	s.mu.Unlock()
	s.userChanged(r.ID, "")
	s.rolesChanged() // its group memberships are gone
	return true, nil
}
