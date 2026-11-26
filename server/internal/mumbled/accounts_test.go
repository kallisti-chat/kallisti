package mumbled

import (
	"net/netip"
	"testing"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

func checkPlain(encoded, password string) bool { return encoded == "hash:"+password }

func TestAuthentication(t *testing.T) {
	alice := &Registered{ID: 5, Name: "Alice", CertHash: "aaa"}
	bob := &Registered{ID: 6, Name: "Bob", PasswordHash: "hash:secret"}
	super := &Registered{ID: 0, Name: "SuperUser"}
	for _, c := range []struct {
		name           string
		byName, byHash *Registered
		password, hash string
		wantID         int64 // -1: guest
		wantFail       bool
	}{
		{"guest", nil, nil, "", "", -1, false},
		{"guest with a certificate", nil, nil, "", "zzz", -1, false},
		{"registered by certificate", alice, alice, "", "aaa", 5, false},
		{"certificate wins over the typed name", nil, alice, "", "aaa", 5, false},
		{"registered name, other certificate", alice, nil, "", "zzz", 0, true},
		{"registered name, no certificate", alice, nil, "", "", 0, true},
		{"password user", bob, nil, "secret", "", 6, false},
		{"password user, wrong password", bob, nil, "nope", "", 0, true},
		{"password for a certificate user", alice, alice, "x", "aaa", 0, true},
		{"superuser needs a password", super, nil, "", "", 0, true},
	} {
		r, ok := authenticate(c.byName, c.byHash, c.password, c.hash, checkPlain)
		gotID := int64(-1)
		if r != nil {
			gotID = int64(r.ID)
		}
		if ok == c.wantFail || (!c.wantFail && gotID != c.wantID) {
			t.Errorf("%s: got id %d ok %v", c.name, gotID, ok)
		}
	}
}

func TestGhosts(t *testing.T) {
	ip1, ip2 := netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2")
	online := []Presence{
		{Session: 1, Name: "alice", Registered: true, UserID: 5, Host: ip1, Hash: "aaa"},
		{Session: 2, Name: "guest", Host: ip1, Hash: ""},
	}
	for _, c := range []struct {
		name      string
		new       Presence
		wantGhost uint32
		wantInUse bool
	}{
		{"registered user from another device", Presence{Name: "alice", Registered: true, UserID: 5, Host: ip2, Hash: "aaa"}, 1, false},
		{"guest name from the same IP", Presence{Name: "Guest", Host: ip1}, 2, false},
		{"guest name from another IP", Presence{Name: "guest", Host: ip2}, 0, true},
		{"new name", Presence{Name: "carol", Host: ip2}, 0, false},
	} {
		ghost, inUse := findGhost(online, c.new)
		if ghost != c.wantGhost || inUse != c.wantInUse {
			t.Errorf("%s: ghost %d in use %v", c.name, ghost, inUse)
		}
	}
}

func waitForUserID(t *testing.T, c *testClient, session uint32) uint32 {
	t.Helper()
	for {
		var m pb.UserState
		proto.Unmarshal(c.expect(mumble.TypeUserState), &m)
		if m.GetSession() == session && m.UserId != nil {
			return m.GetUserId()
		}
	}
}

func TestSelfRegistration(t *testing.T) {
	db := testDB(t)
	addr := startServer(t, "127.0.0.1", Config{DB: db}, openACL)
	cert := clientCert(t)
	alice := dialAs(t, addr, dialOpts{name: "alice", cert: cert})
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), UserId: proto.Uint32(0)})
	id := waitForUserID(t, alice, alice.session)
	if id == 0 {
		t.Fatal("registered as SuperUser")
	}
	games := alice.createChannel(0, "Games", false)
	alice.move(alice.session, games)
	alice.waitForMove(alice.session, games)
	alice.conn.Close()
	// The last channel is saved when the server sees the disconnect.
	eventually(t, func() bool { r, err := db.UserByID(id); return err == nil && r.LastChannel == games })

	// Back with the same certificate, under another name: the registered
	// name, the same id, and the last channel.
	again, _ := tryDialWith(t, addr, dialOpts{name: "whatever", cert: cert})
	for {
		var m pb.UserState
		proto.Unmarshal(again.expect(mumble.TypeUserState), &m)
		if m.GetName() == "alice" {
			if m.GetUserId() != id || m.GetChannelId() != games || m.GetHash() == "" {
				t.Fatalf("again: %v", &m)
			}
			break
		}
	}
	// Nobody else can use the name.
	other, _ := tryDialWith(t, addr, dialOpts{name: "Alice", cert: clientCert(t)})
	var r pb.Reject
	proto.Unmarshal(other.expect(mumble.TypeReject), &r)
	if r.GetType() != pb.Reject_WrongUserPW {
		t.Fatalf("reject %v", &r)
	}
}

func TestRegistrationNeedsACertificate(t *testing.T) {
	addr := startWith(t, "127.0.0.1", Config{DB: testDB(t)})
	guest := dialAs(t, addr, dialOpts{name: "guest"}) // no certificate
	guest.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(guest.session), UserId: proto.Uint32(0)})
	var d pb.PermissionDenied
	proto.Unmarshal(guest.expect(mumble.TypePermissionDenied), &d)
	if d.GetType() != pb.PermissionDenied_MissingCertificate {
		t.Fatalf("denied %v", &d)
	}
}

func TestARegisteredUserReplacesItsOtherDevice(t *testing.T) {
	db := testDB(t)
	addr := startWith(t, "127.0.0.1", Config{DB: db})
	cert := clientCert(t)
	first := dialAs(t, addr, dialOpts{name: "alice", cert: cert})
	first.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(first.session), UserId: proto.Uint32(0)})
	waitForUserID(t, first, first.session)
	dialAs(t, addr, dialOpts{name: "alice", cert: cert})
	var m pb.UserRemove
	proto.Unmarshal(first.expect(mumble.TypeUserRemove), &m)
	if m.GetSession() != first.session || m.GetReason() == "" {
		t.Fatalf("first device got %v", &m)
	}
}

func TestSuperUserLogsInWithItsPassword(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin pw")
	addr := startWith(t, "127.0.0.1", Config{DB: db})
	su := dialAs(t, addr, dialOpts{name: "SuperUser", password: "admin pw"})
	if su.session == 0 {
		t.Fatal("no session")
	}
	bad, _ := tryDialWith(t, addr, dialOpts{name: "SuperUser", password: "wrong"})
	var r pb.Reject
	proto.Unmarshal(bad.expect(mumble.TypeReject), &r)
	if r.GetType() != pb.Reject_WrongUserPW {
		t.Fatalf("reject %v", &r)
	}
}

func TestServerPasswordForGuests(t *testing.T) {
	addr := startWith(t, "127.0.0.1", Config{Password: "letmein"})
	bad, _ := tryDialWith(t, addr, dialOpts{name: "guest"})
	var r pb.Reject
	proto.Unmarshal(bad.expect(mumble.TypeReject), &r)
	if r.GetType() != pb.Reject_WrongServerPW {
		t.Fatalf("reject %v", &r)
	}
	dialAs(t, addr, dialOpts{name: "guest2", password: "letmein"})
}

func TestQueryUsers(t *testing.T) {
	addr, db := adminServer(t)
	id, _ := db.RegisterUser("Alice", "aaa")
	guest := dial(t, addr, "bob")
	guest.send(mumble.TypeQueryUsers, &pb.QueryUsers{Ids: []uint32{id}})
	guest.expectDenied(pb.PermissionDenied_Permission) // only for ACL editors
	c := superUser(t, addr)
	c.send(mumble.TypeQueryUsers, &pb.QueryUsers{Ids: []uint32{id}, Names: []string{"superuser", "nobody"}})
	var m pb.QueryUsers
	proto.Unmarshal(c.expect(mumble.TypeQueryUsers), &m)
	got := map[uint32]string{}
	for i := range m.Ids {
		got[m.Ids[i]] = m.Names[i]
	}
	if got[id] != "Alice" || got[0] != "SuperUser" || len(got) != 2 {
		t.Fatalf("query users %v", got)
	}
}

func TestAKickedSessionCannotActAnyMore(t *testing.T) {
	addr := startWith(t, "127.0.0.1", Config{DB: testDB(t)})
	cert := clientCert(t)
	old := dialAs(t, addr, dialOpts{name: "foo", cert: cert})
	dialAs(t, addr, dialOpts{name: "foo", cert: cert}) // replaces old
	// Messages that were still on the way must not reach the server.
	old.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(old.session), UserId: proto.Uint32(0)})
	old.send(mumble.TypeChannelState, &pb.ChannelState{Parent: proto.Uint32(0), Name: proto.String("Ghost"), Temporary: proto.Bool(true)})
	watcher := dial(t, addr, "watcher")
	watcher.send(mumble.TypePing, &pb.Ping{Timestamp: proto.Uint64(1)})
	watcher.expect(mumble.TypePing) // the server is alive
}

func TestFailedLoginsAreLimitedPerIP(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "right")
	addr := startWith(t, "127.0.0.1", Config{DB: db})
	for range maxLoginFailures {
		c, _ := tryDialWith(t, addr, dialOpts{name: "SuperUser", password: "wrong"})
		c.expect(mumble.TypeReject)
	}
	c, _ := tryDialWith(t, addr, dialOpts{name: "SuperUser", password: "right"})
	var r pb.Reject
	proto.Unmarshal(c.expect(mumble.TypeReject), &r)
	if r.GetType() != pb.Reject_None || r.GetReason() == "" {
		t.Fatalf("after many failures: %v", &r)
	}
}

func TestAnotherDeviceKeepsTheChannel(t *testing.T) {
	db := testDB(t)
	addr := startServer(t, "127.0.0.1", Config{DB: db}, openACL)
	cert := clientCert(t)
	first := dialAs(t, addr, dialOpts{name: "alice", cert: cert})
	first.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(first.session), UserId: proto.Uint32(0)})
	waitForUserID(t, first, first.session)
	games := first.createChannel(0, "Games", false)
	first.move(first.session, games)
	first.waitForMove(first.session, games)
	second, _ := tryDialWith(t, addr, dialOpts{name: "alice", cert: cert})
	for {
		var m pb.UserState
		proto.Unmarshal(second.expect(mumble.TypeUserState), &m)
		if m.GetName() == "alice" && m.GetSession() != first.session {
			if m.GetChannelId() != games {
				t.Fatalf("second device in channel %d", m.GetChannelId())
			}
			return
		}
	}
}
