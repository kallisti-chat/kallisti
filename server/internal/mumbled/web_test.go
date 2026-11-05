package mumbled

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
	"kallisti.chat/server/internal/names"
)

// webClient joins through ServeConn, as the Kallisti server's web bridge
// does: no TLS, the real IP and maybe a web account from the bridge.
func webClient(t *testing.T, s *Server, origin Origin, name string) *testClient {
	t.Helper()
	a, b := net.Pipe()
	go s.ServeConn(b, origin)
	c := startClient(t, a, 1, 5, name, "")
	t.Cleanup(func() { a.Close() })
	return c
}

func joined(t *testing.T, c *testClient) *testClient {
	t.Helper()
	var m pb.ServerSync
	proto.Unmarshal(c.expect(mumble.TypeServerSync), &m)
	c.session = m.GetSession()
	return c
}

func TestWebClientsJoinInProcess(t *testing.T) {
	s, addr := startServerAndListen(t, "127.0.0.1", Config{}, nil)
	native := dial(t, addr, "native")
	web := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5")}, "webby"))
	web.talk(0)
	if h := parseAudio(t, native.expect(mumble.TypeUDPTunnel)); h.sender != web.session {
		t.Fatalf("native heard %+v", h)
	}
	// Two web guests with one name from different IPs: the name is in use.
	other := webClient(t, s, Origin{Host: netip.MustParseAddr("198.51.100.7")}, "webby")
	var r pb.Reject
	proto.Unmarshal(other.expect(mumble.TypeReject), &r)
	if r.GetType() != pb.Reject_UsernameInUse {
		t.Fatalf("reject %v", &r)
	}
}

func TestIPBansApplyToWebClients(t *testing.T) {
	s, _ := startServerAndListen(t, "127.0.0.1", Config{}, nil)
	ip := netip.MustParseAddr("203.0.113.9")
	s.bans.Store(&[]BanEntry{{Prefix: netip.PrefixFrom(ip, 32)}})
	c := webClient(t, s, Origin{Host: ip}, "banned")
	if _, ok := <-c.frames; ok {
		// The connection may send nothing at all, or a Reject.
		var r pb.Reject
		proto.Unmarshal(c.expect(mumble.TypeReject), &r)
	}
	joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.10")}, "fine"))
}

func TestWebAccountsAreRegistrations(t *testing.T) {
	db := testDB(t)
	reserved := map[string]bool{"alice": true}
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db, ReservedName: func(n string) bool { return reserved[n] }}, nil)
	account := &WebAccount{Name: "Alice", CertHash: "abc123"}
	web := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: account}, "Alice"))
	reg, _ := db.UserByHash("abc123")
	if reg == nil {
		t.Fatal("the account was not registered")
	}
	id := reg.ID
	web.conn.Close()
	again := webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: account}, "Alice")
	for {
		var m pb.UserState
		proto.Unmarshal(again.expect(mumble.TypeUserState), &m)
		if m.GetName() == "Alice" {
			if m.UserId == nil || m.GetUserId() != id {
				t.Fatalf("again: %v", &m)
			}
			break
		}
	}
	// Native clients and guests cannot use an account's name.
	native, _ := tryDial(t, addr, "alice")
	var r pb.Reject
	proto.Unmarshal(native.expect(mumble.TypeReject), &r)
	if r.GetType() != pb.Reject_UsernameInUse && r.GetType() != pb.Reject_WrongUserPW {
		t.Fatalf("native reject %v", &r)
	}
	guest := webClient(t, s, Origin{Host: netip.MustParseAddr("198.51.100.1")}, "ALICE")
	proto.Unmarshal(guest.expect(mumble.TypeReject), &r)
	if r.GetType() != pb.Reject_UsernameInUse && r.GetType() != pb.Reject_WrongUserPW {
		t.Fatalf("guest reject %v", &r)
	}
}

func TestReservedNamesCannotBeSelfRegistered(t *testing.T) {
	db := testDB(t)
	addr := startWith(t, "127.0.0.1", Config{DB: db, ReservedName: func(n string) bool { return n == "bob" }})
	// "bob" belongs to a web account that never used Mumble: a native
	// client cannot even join under that name.
	c, _ := tryDial(t, addr, "Bob")
	var r pb.Reject
	proto.Unmarshal(c.expect(mumble.TypeReject), &r)
	if r.GetType() != pb.Reject_UsernameInUse {
		t.Fatalf("reject %v", &r)
	}
}

func TestAccountNamesCannotBeRegisteredOrTaken(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	reserved := map[string]bool{"carol": true}
	cfg := Config{DB: db, ReservedName: func(n string) bool { return reserved[n] }}
	s, addr := startServerAndListen(t, "127.0.0.1", cfg, nil)
	// A guest "bob" whose name becomes an account while it is online.
	bob := dial(t, addr, "bob")
	reserved["bob"] = true
	bob.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(bob.session), UserId: proto.Uint32(0)})
	bob.expectDenied(pb.PermissionDenied_UserName)
	// An admin cannot rename a registration to an account's name.
	id, _ := db.RegisterUser("alice", "aaa")
	su := superUser(t, addr)
	su.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(id), Name: proto.String("Carol")}}})
	su.expectDenied(pb.PermissionDenied_Text)
	// An account whose registration has another name is refused clearly.
	db.RenameUser(id, "dave")
	c := webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "alice", CertHash: "aaa"}}, "alice")
	var r pb.Reject
	proto.Unmarshal(c.expect(mumble.TypeReject), &r)
	if r.GetReason() == "" || r.GetType() == pb.Reject_AuthenticatorFail {
		t.Fatalf("reject %v", &r)
	}
}

func TestARejectArrivesDespiteExtraBytes(t *testing.T) {
	s, _ := startServerAndListen(t, "127.0.0.1", Config{}, nil)
	a, b := net.Pipe()
	defer a.Close()
	go s.ServeConn(b, Origin{Host: netip.MustParseAddr("203.0.113.5")})
	// One write with Version, Authenticate (an invalid name) and more.
	v1 := uint32(1<<16 | 5<<8)
	hello := append(mumble.MessageFrame(mumble.TypeVersion, &pb.Version{VersionV1: &v1}),
		mumble.MessageFrame(mumble.TypeAuthenticate, &pb.Authenticate{Username: proto.String("two words")})...)
	hello = append(hello, mumble.MessageFrame(mumble.TypePing, &pb.Ping{})...)
	go a.Write(hello)
	c := startClientReader(t, a)
	var r pb.Reject
	proto.Unmarshal(c.expectWithin(mumble.TypeReject, 2*time.Second), &r)
	if r.GetType() != pb.Reject_InvalidUsername {
		t.Fatalf("reject %v", &r)
	}
}

func TestShutdownSavesTheLastChannel(t *testing.T) {
	db := testDB(t)
	s, _ := startServerAndListen(t, "127.0.0.1", Config{DB: db}, openACL)
	account := &WebAccount{Name: "alice", CertHash: "aaa"}
	alice := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: account}, "alice"))
	games := alice.createChannel(0, "Games", false)
	alice.move(alice.session, games)
	alice.waitForMove(alice.session, games)
	s.Close()
	r, _ := db.UserByHash("aaa")
	if r == nil || r.LastChannel != games {
		t.Fatalf("after shutdown: %+v", r)
	}
}

func TestTheLobbyDoesNotRestoreTheLastChannel(t *testing.T) {
	// Kallisti: users arrive in the lobby and join voice on purpose, also
	// when they were in a voice channel last time.
	db := testDB(t)
	if err := db.InitLobby(); err != nil {
		t.Fatal(err)
	}
	s, _ := startServerAndListen(t, "127.0.0.1", Config{DB: db}, nil)
	account := &WebAccount{Name: "alice", CertHash: "aaa"}
	first := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: account}, "alice"))
	var general uint32
	s.mu.Lock()
	for _, c := range s.state.Channels {
		if c.Name == "General" {
			general = c.ID
		}
	}
	s.mu.Unlock()
	first.move(first.session, general)
	first.waitForMove(first.session, general)
	first.conn.Close()
	eventually(t, func() bool { r, _ := db.UserByHash("aaa"); return r != nil && r.LastChannel == general })
	again := webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: account}, "alice")
	for {
		var m pb.UserState
		proto.Unmarshal(again.expect(mumble.TypeUserState), &m)
		if m.GetName() == "alice" {
			if m.GetChannelId() != 0 {
				t.Fatalf("back in channel %d", m.GetChannelId())
			}
			return
		}
	}
}

func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAccountRegistrationsKeepTheirName(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	reserved := map[string]bool{"alice": true}
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db, ReservedName: func(n string) bool { return reserved[n] }}, nil)
	joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "alice", CertHash: "aaa"}}, "alice"))
	reg, _ := db.UserByHash("aaa")
	su := superUser(t, addr)
	// Renaming it would lock the account out (its name is the account's).
	su.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(reg.ID), Name: proto.String("mallory")}}})
	su.expectDenied(pb.PermissionDenied_Text)
	if r, _ := db.UserByHash("aaa"); r.Name != "alice" {
		t.Fatalf("renamed to %q", r.Name)
	}
}

// Unregistering an account's registration deletes the account (else it
// would log in again and register anew). Only admins delete users.
func TestUnregisteringAnAccountDeletesIt(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	reserved := map[string]bool{"alice": true, "dave": true, "erin": true}
	deleted := make(chan string, 4)
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db, ReservedName: func(n string) bool { return reserved[n] },
		WebAccountExists: func(_, hash string) bool { return hash == "aaa" || hash == "eee" },
		DeleteAccount: func(name, hash string) error {
			if name == "erin" {
				return errors.New("disk full")
			}
			if hash == "aaa" { // dave's account has another certificate
				deleted <- name
			}
			return nil
		}}, nil)
	joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "alice", CertHash: "aaa"}}, "alice"))
	reg, _ := db.UserByHash("aaa")
	su := superUser(t, addr)
	bob, bobID := registered(t, addr, "bob")
	s.SetModerator(su.session, bobID, true)
	carol, _ := db.RegisterUser("carol", "ccc")
	bob.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(reg.ID)}, {UserId: proto.Uint32(carol)}}})
	bob.expectDenied(pb.PermissionDenied_Text)
	bob.expectDenied(pb.PermissionDenied_Text)
	if r, _ := db.UserByHash("aaa"); r == nil {
		t.Fatal("a moderator deleted an account")
	}
	if r, _ := db.UserByHash("ccc"); r == nil {
		t.Fatal("a moderator deleted a Mumble user")
	}
	// The account stays when it cannot be deleted, and so does its
	// registration.
	erin, _ := db.RegisterUser("erin", "eee")
	su.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(erin)}}})
	su.expectDenied(pb.PermissionDenied_Text)
	if r, _ := db.UserByHash("eee"); r == nil {
		t.Fatal("registration removed without its account")
	}
	// No account to delete: a Mumble-only registration, also one with an
	// account's name but another certificate.
	dave, _ := db.RegisterUser("dave", "ddd")
	su.send(mumble.TypeUserList, &pb.UserList{Users: []*pb.UserList_User{{UserId: proto.Uint32(carol)}, {UserId: proto.Uint32(dave)}, {UserId: proto.Uint32(reg.ID)}}})
	eventually(t, func() bool { r, _ := db.UserByHash("aaa"); return r == nil })
	if r, _ := db.UserByHash("ddd"); r != nil {
		t.Error("dave stays")
	}
	if got := <-deleted; got != "alice" || len(deleted) != 0 {
		t.Fatalf("deleted %q, %d more", got, len(deleted))
	}
}

// A web account deleted meanwhile (a guest account cleaned up while its
// connection was on the way) is not registered again.
func TestDeletedWebAccountsAreNotRegistered(t *testing.T) {
	db := testDB(t)
	s, _ := startServerAndListen(t, "127.0.0.1", Config{DB: db, WebAccountExists: func(_, hash string) bool { return hash != "gone" }}, nil)
	c := webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "ghost", CertHash: "gone"}}, "ghost")
	var r pb.Reject
	proto.Unmarshal(c.expect(mumble.TypeReject), &r)
	if reg, _ := db.UserByHash("gone"); reg != nil || r.GetReason() == "" {
		t.Fatalf("registered %+v, reject %v", reg, &r)
	}
}

func TestMumbleClientsAndWebSessionsAreToldApart(t *testing.T) {
	db := testDB(t)
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db}, nil)
	native, nativeID := registered(t, addr, "native")
	web := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "webby", CertHash: "w1"}}, "webby"))
	if u, ok := s.MumbleClient(native.session); !ok || !u.Registered || u.UserID != nativeID {
		t.Fatalf("native: %+v %v", u, ok)
	}
	// Who it is still comes back: slow mode counts its binary texts
	// against the account.
	if u, ok := s.MumbleClient(web.session); ok || !u.Registered || u.UserID == 0 {
		t.Fatalf("web: %+v %v", u, ok)
	}
	if _, ok := s.MumbleClient(9999); ok {
		t.Fatal("unknown session")
	}
}

// accountLogin is a Config.AccountLogin for tests: accounts by name with
// their password; guest accounts have none.
func accountLogin(accounts map[string]string, guests map[string]bool) func(name, pw string) (*WebAccount, bool) {
	return func(name, pw string) (*WebAccount, bool) {
		key := names.Key(name)
		want, ok := accounts[key]
		if !ok || guests[key] {
			return nil, true
		}
		if pw != want {
			return nil, false
		}
		return &WebAccount{Name: key, CertHash: "cert-" + key}, true
	}
}

func TestMumbleClientsLogInWithAnAccountPassword(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin pw")
	reserved := map[string]bool{"alice": true, "bob": true, "gus": true}
	cfg := Config{DB: db, Password: "server pw", ReservedName: func(n string) bool { return reserved[n] },
		AccountLogin: accountLogin(map[string]string{"alice": "secret", "bob": "bobpw", "gus": "", "superuser": "other"}, map[string]bool{"gus": true})}
	s, addr := startServerAndListen(t, "127.0.0.1", cfg, nil)
	reject := func(c *testClient, _ error) *pb.Reject {
		var r pb.Reject
		proto.Unmarshal(c.expect(mumble.TypeReject), &r)
		return &r
	}

	// Not yet registered: registered as the account (no server password).
	alice := dialAs(t, addr, dialOpts{name: "alice", password: "secret", cert: clientCert(t)})
	reg, _ := db.UserByHash("cert-alice")
	if reg == nil {
		t.Fatal("the account was not registered")
	}
	if u, _ := s.MumbleClient(alice.session); !u.Registered || u.UserID != reg.ID {
		t.Fatalf("alice: %+v, want user %d", u, reg.ID)
	}
	// Already registered (through the web): the same user.
	joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "bob", CertHash: "cert-bob"}}, "bob"))
	bobReg, _ := db.UserByHash("cert-bob")
	bob := dialAs(t, addr, dialOpts{name: "Bob", password: "bobpw"})
	if u, _ := s.MumbleClient(bob.session); !u.Registered || u.UserID != bobReg.ID {
		t.Fatalf("bob: %+v, want user %d", u, bobReg.ID)
	}

	// A wrong password is refused and counted.
	if r := reject(tryDialWith(t, addr, dialOpts{name: "alice", password: "guess"})); r.GetType() != pb.Reject_WrongUserPW {
		t.Fatalf("wrong password: %v", r)
	}
	s.logins.mu.Lock()
	failed := s.logins.failures[netip.MustParseAddr("127.0.0.1")] != nil
	s.logins.mu.Unlock()
	if !failed {
		t.Fatal("the failure was not counted")
	}
	// Guest accounts have no password: the name stays theirs.
	if r := reject(tryDialWith(t, addr, dialOpts{name: "gus", password: "server pw"})); r.GetType() != pb.Reject_UsernameInUse {
		t.Fatalf("guest account: %v", r)
	}
	// Mumble registrations with a password as before, also under an
	// account's name.
	if su := dialAs(t, addr, dialOpts{name: "SuperUser", password: "admin pw"}); su.session == 0 {
		t.Fatal("no SuperUser")
	}
	// A banned account stays banned with a Mumble client.
	s.bans.Store(&[]BanEntry{{Hash: "cert-alice"}})
	if r := reject(tryDialWith(t, addr, dialOpts{name: "alice", password: "secret"})); !strings.Contains(r.GetReason(), "banned") {
		t.Fatalf("banned: %v", r)
	}
}

func TestEndingAnAccountsPasswordLoginsKicksItsMumbleClients(t *testing.T) {
	db := testDB(t)
	cfg := Config{DB: db, AccountLogin: accountLogin(map[string]string{"alice": "secret", "bob": "bobpw"}, nil)}
	s, addr := startServerAndListen(t, "127.0.0.1", cfg, nil)
	alice := dialAs(t, addr, dialOpts{name: "alice", password: "secret", cert: clientCert(t)})
	bob := dialAs(t, addr, dialOpts{name: "bob", password: "bobpw", cert: clientCert(t)})
	web := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "alice", CertHash: "cert-alice"}}, "alice"))
	guest := dialAs(t, addr, dialOpts{name: "guest"}) // no certificate

	s.EndPasswordLogins("") // an account without a certificate
	s.EndPasswordLogins("cert-alice")
	var m pb.UserRemove
	proto.Unmarshal(alice.expect(mumble.TypeUserRemove), &m)
	if m.GetSession() != alice.session {
		t.Fatalf("removed %d, want alice %d", m.GetSession(), alice.session)
	}
	online := func(session uint32) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.state.Users[session] != nil
	}
	// The web session ends with its own login (see bridge.Revoke); others stay.
	if online(alice.session) || !online(bob.session) || !online(web.session) || !online(guest.session) {
		t.Fatalf("online: alice %v, bob %v, web %v, guest %v", online(alice.session), online(bob.session), online(web.session), online(guest.session))
	}
}

// The desktop app's bridge sends a web session's voice over UDP: the
// server takes it from the session's IP and key, and sends voice back over
// UDP, as to a Mumble client.
func TestWebSessionsGetVoiceOverUDP(t *testing.T) {
	s, addr := startServerAndListen(t, "127.0.0.1", Config{}, openACL)
	transport := func(session uint32) string {
		for _, st := range s.ConnectionStats() {
			if st.Session == session {
				return st.Transport
			}
		}
		return ""
	}
	web := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("127.0.0.1")}, "webby"))
	native := dial(t, addr, "native")
	web.openUDP(addr)
	web.sendUDP(audio(3, []byte{0xfc, 3}))
	if h := parseAudio(t, native.expect(mumble.TypeUDPTunnel)); h.sender != web.session || h.frame != 3 {
		t.Fatalf("native heard %+v", h)
	}
	if got := transport(web.session); got != "websocket+udp" {
		t.Fatalf("transport %q", got)
	}
	native.conn.Write(mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(4, []byte{0xfc, 4})))
	if h := parseAudio(t, web.readUDP()); h.sender != native.session || h.frame != 4 {
		t.Fatalf("web heard %+v", h)
	}
	web.expectNone(mumble.TypeUDPTunnel)
	// A listener's bridge announces UDP with an empty voice packet to the
	// loopback target once its pings come back: it then hears over UDP
	// too, and nobody hears the packet.
	listener := joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("127.0.0.1")}, "listener"))
	listener.openUDP(addr)
	listener.sendUDP([]byte{0x00, 0x08, 31})
	eventually(t, func() bool { return transport(listener.session) == "websocket+udp" })
	native.conn.Write(mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(5, []byte{0xfc, 5})))
	if h := parseAudio(t, listener.readUDP()); h.sender != native.session || h.frame != 5 {
		t.Fatalf("listener heard %+v", h)
	}
	web.expectNone(mumble.TypeUDPTunnel)
	native.expectNone(mumble.TypeUDPTunnel)
}
