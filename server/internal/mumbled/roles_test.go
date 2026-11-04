package mumbled

import (
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// registered joins with a certificate and registers itself.
func registered(t *testing.T, addr, name string) (*testClient, uint32) {
	t.Helper()
	c := dialAs(t, addr, dialOpts{name: name, cert: clientCert(t)})
	c.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(c.session), UserId: proto.Uint32(0)})
	return c, waitForUserID(t, c, c.session)
}

func rolesServer(t *testing.T) (*Server, string) {
	t.Helper()
	db := testDB(t)
	setAdminPassword(db, "admin")
	return startServerAndListen(t, "127.0.0.1", Config{DB: db}, nil)
}

func TestModeratorsDoAlmostAllThatAdminsDo(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	if err := s.SetModerator(su.session, aliceID, true); err != nil {
		t.Fatal(err)
	}
	// Channels: make, rename, delete.
	games := alice.createChannel(0, "Games", false)
	alice.send(mumble.TypeChannelState, &pb.ChannelState{ChannelId: proto.Uint32(games), Name: proto.String("Fun")})
	for {
		var m pb.ChannelState
		proto.Unmarshal(alice.expect(mumble.TypeChannelState), &m)
		if m.GetChannelId() == games && m.GetName() == "Fun" {
			break
		}
	}
	// Kick a guest.
	guest := dial(t, addr, "guest")
	alice.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(guest.session), Reason: proto.String("bye")})
	guest.expect(mumble.TypeUserRemove)
}

// Each test client sends at most 5 messages at once (the server's limit).
func TestModeratorsCannotHandOutRightsOrActOnStaff(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	// No changes to root's ACL (roles live there) and to the ban list.
	alice.setACL(0, true, nil, nil)
	alice.expectDenied(pb.PermissionDenied_Permission)
	alice.send(mumble.TypeBanList, &pb.BanList{Bans: []*pb.BanList_BanEntry{{Address: make([]byte, 16), Mask: proto.Uint32(0)}}})
	alice.expectDenied(pb.PermissionDenied_Permission)
	_, bobID := registered(t, addr, "bob")
	if err := s.SetModerator(alice.session, bobID, true); err == nil {
		t.Fatal("a moderator made a moderator")
	}
	// Admins and moderators are safe from moderators.
	carol, carolID := registered(t, addr, "carol")
	s.SetModerator(su.session, carolID, true)
	alice.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(carol.session)})
	alice.expectDenied(pb.PermissionDenied_Permission)
	alice.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(carolID)}}})
	alice.expectDenied(pb.PermissionDenied_Permission)
}

func TestRolesListsAndUnmakesModerators(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	_, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	if _, mods := s.Roles(); len(mods) != 1 || mods[0] != aliceID {
		t.Fatalf("moderators %v", mods)
	}
	s.SetModerator(su.session, aliceID, false)
	if _, mods := s.Roles(); len(mods) != 0 {
		t.Fatalf("after unmaking: %v", mods)
	}
}

func TestInitRolesAddsTheModeratorGroupOnce(t *testing.T) {
	db := testDB(t)
	// An older database: root has Murmur's ACL without moderators.
	acls, groups := DefaultRootACL()
	root := &Channel{ID: 0, Name: "Root", ACLs: acls[:0:0], Groups: map[string]*Group{"admin": groups["admin"]}}
	for _, a := range acls {
		if a.Group != "moderator" {
			root.ACLs = append(root.ACLs, a)
		}
	}
	db.SaveChannel(root)
	for range 2 {
		if err := db.InitRoles(); err != nil {
			t.Fatal(err)
		}
	}
	saved, _ := db.Channels()
	s := NewState("Root", saved)
	got := s.Channels[0]
	n := 0
	for _, a := range got.ACLs {
		if a.Group == "moderator" {
			n++
		}
	}
	if got.Groups["moderator"] == nil || n != 2 {
		t.Fatalf("root after InitRoles: %+v", got)
	}
}

func TestNewModeratorsLearnTheirRootPermissions(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	games := su.createChannel(0, "Games", false)
	alice, aliceID := registered(t, addr, "alice")
	alice.move(alice.session, games)
	alice.waitForMove(alice.session, games)
	s.SetModerator(su.session, aliceID, true)
	for {
		var m pb.PermissionQuery
		proto.Unmarshal(alice.expect(mumble.TypePermissionQuery), &m)
		// The move sent root's permissions before too (without Kick).
		if m.GetChannelId() == 0 && Perm(m.GetPermissions())&Kick != 0 {
			return
		}
	}
}

func TestModeratorsReadBansAndEditSubchannelACLs(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	games := su.createChannel(0, "Games", false)
	alice, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	alice.send(mumble.TypeBanList, &pb.BanList{Query: proto.Bool(true)})
	alice.expect(mumble.TypeBanList)
	// As in Murmur: Write in a channel allows its ACL (not root's).
	alice.setACL(games, true, []*pb.ACL_ChanACL{chanACL("all", true, true, 0, Enter)}, nil)
	for {
		var m pb.ChannelState
		proto.Unmarshal(alice.expect(mumble.TypeChannelState), &m)
		if m.GetChannelId() == games && m.GetIsEnterRestricted() {
			break
		}
	}
}

func TestUsersWithWriteInRootAreStaff(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	dave, daveID := registered(t, addr, "dave")
	s.mu.Lock()
	root := s.state.Channels[0]
	root.ACLs = append(root.ACLs, ACL{ApplyHere: true, ApplySubs: true, HasUser: true, UserID: daveID, Allow: Write})
	s.mu.Unlock()
	alice, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	alice.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(dave.session)})
	alice.expectDenied(pb.PermissionDenied_Permission)
}

func TestAdminsMakeAdminsOnlySuperUserRemovesThem(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	bob, bobID := registered(t, addr, "bob")
	_, carolID := registered(t, addr, "carol")
	if err := s.SetAdmin(bob.session, carolID, true); err == nil {
		t.Fatal("a non-admin made an admin")
	}
	if err := s.SetAdmin(su.session, aliceID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAdmin(alice.session, bobID, true); err != nil {
		t.Fatal("an admin could not make an admin:", err)
	}
	if admins, _ := s.Roles(); len(admins) != 2 {
		t.Fatalf("admins %v", admins)
	}
	// Admins cannot remove each other; SuperUser can.
	if err := s.SetAdmin(alice.session, bobID, false); err == nil {
		t.Fatal("an admin removed an admin")
	}
	if err := s.SetAdmin(su.session, bobID, false); err != nil {
		t.Fatal(err)
	}
	if admins, _ := s.Roles(); len(admins) != 1 || admins[0] != aliceID {
		t.Fatalf("admins %v", admins)
	}
	// Saved, as root's admin group.
	saved, _ := s.cfg.DB.Channels()
	if g := saved[0].Groups["admin"]; saved[0].ID != 0 || !g.Add[aliceID] || g.Add[bobID] {
		t.Fatalf("saved root %+v", saved[0])
	}
}

func TestSuperUserIsNoRoleToChange(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	for _, on := range []bool{true, false} {
		if err := s.SetAdmin(su.session, 0, on); err == nil {
			t.Fatalf("changed SuperUser (on %v)", on)
		}
	}
}

// Root's ACL holds the roles: only SuperUser changes it, so admins cannot
// remove each other with a Mumble client.
func TestOnlySuperUserEditsRootACL(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	_, bobID := registered(t, addr, "bob")
	s.SetAdmin(su.session, aliceID, true)
	s.SetAdmin(su.session, bobID, true)
	only := func(id uint32) []*pb.ACL_ChanGroup {
		return []*pb.ACL_ChanGroup{{Name: proto.String("admin"), Add: []uint32{id}}}
	}
	alice.setACL(0, true, nil, only(aliceID))
	alice.expectDenied(pb.PermissionDenied_Permission)
	alice.send(mumble.TypeACL, &pb.ACL{ChannelId: proto.Uint32(0), Query: proto.Bool(true)})
	alice.expect(mumble.TypeACL)
	if admins, _ := s.Roles(); !slices.Contains(admins, bobID) {
		t.Fatalf("an admin removed an admin: %v", admins)
	}
	su.setACL(0, true, []*pb.ACL_ChanACL{chanACL("admin", true, true, Write, 0)}, only(aliceID))
	for deadline := time.Now().Add(2 * time.Second); ; {
		if admins, _ := s.Roles(); !slices.Contains(admins, bobID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SuperUser could not change root's ACL")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Admins cannot delete each other (as they cannot remove each other's
// role); SuperUser can.
func TestOnlySuperUserDeletesAdmins(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	_, bobID := registered(t, addr, "bob")
	for _, id := range []uint32{aliceID, bobID} {
		if err := s.SetAdmin(su.session, id, true); err != nil {
			t.Fatal(err)
		}
	}
	alice.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(bobID)}}})
	alice.expectDenied(pb.PermissionDenied_Text)
	if r, _ := s.cfg.DB.UserByID(bobID); r == nil {
		t.Fatal("an admin deleted an admin")
	}
	su.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(bobID)}}})
	eventually(t, func() bool { r, _ := s.cfg.DB.UserByID(bobID); return r == nil })
}
