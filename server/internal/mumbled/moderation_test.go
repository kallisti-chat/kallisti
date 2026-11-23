package mumbled

import (
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"net/netip"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

func TestKick(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	alice.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(bob.session), Reason: proto.String("no")})
	alice.expectDenied(pb.PermissionDenied_Permission)
	alice.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(su.session)})
	alice.expectDenied(pb.PermissionDenied_Permission) // nobody kicks SuperUser
	su.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(bob.session), Reason: proto.String("bye")})
	var m pb.UserRemove
	proto.Unmarshal(bob.expect(mumble.TypeUserRemove), &m)
	if m.GetReason() != "bye" || m.GetActor() != su.session {
		t.Fatalf("bob got %v", &m)
	}
	for {
		proto.Unmarshal(alice.expect(mumble.TypeUserRemove), &m)
		if m.GetSession() == bob.session {
			break
		}
	}
}

func TestBanByCertificateAndIP(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	cert := clientCert(t)
	bad := dialAs(t, addr, dialOpts{name: "bad", cert: cert})
	su.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(bad.session), Reason: proto.String("spam"), Ban: proto.Bool(true)})
	bad.expect(mumble.TypeUserRemove)
	// The ban covers the address too: every test client is 127.0.0.1, so
	// the list is changed to the certificate only.
	su.send(mumble.TypeBanList, &pb.BanList{Query: proto.Bool(true)})
	var list pb.BanList
	proto.Unmarshal(su.expect(mumble.TypeBanList), &list)
	if len(list.Bans) != 1 || list.Bans[0].GetHash() == "" || list.Bans[0].GetMask() != 128 || list.Bans[0].GetReason() != "spam" {
		t.Fatalf("ban list %v", &list)
	}
	entry := list.Bans[0]
	// A new connection from the banned address is closed at once.
	if c, err := tryDialWith(t, addr, dialOpts{name: "other", cert: clientCert(t)}); err == nil {
		select {
		case _, ok := <-c.frames:
			if ok {
				t.Fatal("a banned address got a frame")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a banned address stayed connected")
		}
	}
	// Certificate only: others from this address may come back. Address
	// and mask are required fields: no address is 16 zero bytes, mask 0.
	entry.Address, entry.Mask = make([]byte, 16), proto.Uint32(0)
	su.send(mumble.TypeBanList, &pb.BanList{Bans: []*pb.BanList_BanEntry{entry}})
	su.send(mumble.TypePing, &pb.Ping{})
	su.expect(mumble.TypePing)
	dialAs(t, addr, dialOpts{name: "other", cert: clientCert(t)})
	again, _ := tryDialWith(t, addr, dialOpts{name: "bad", cert: cert})
	var r pb.Reject
	proto.Unmarshal(again.expect(mumble.TypeReject), &r)
	if r.GetReason() == "" {
		t.Fatalf("reject %v", &r)
	}
	alice := dial(t, addr, "alice")
	alice.send(mumble.TypeBanList, &pb.BanList{Query: proto.Bool(true)})
	alice.expectDenied(pb.PermissionDenied_Permission)
}

func TestExpiredBansDoNotCount(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	cert := clientCert(t)
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	su.send(mumble.TypeBanList, &pb.BanList{Bans: []*pb.BanList_BanEntry{{
		Hash: proto.String(certHashOf(cert)), Start: proto.String(old), Duration: proto.Uint32(3600),
		Address: make([]byte, 16), Mask: proto.Uint32(0),
	}}})
	su.send(mumble.TypePing, &pb.Ping{})
	su.expect(mumble.TypePing)
	dialAs(t, addr, dialOpts{name: "probe", cert: cert})
}

func certHashOf(c *tls.Certificate) string {
	sum := sha1.Sum(c.Certificate[0])
	return hex.EncodeToString(sum[:])
}

func TestServerMuteAndDeafen(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	bob.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Mute: proto.Bool(true)})
	bob.expectDenied(pb.PermissionDenied_Permission)
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Mute: proto.Bool(true)})
	for {
		var m pb.UserState
		proto.Unmarshal(bob.expect(mumble.TypeUserState), &m)
		if m.GetSession() == alice.session && m.GetMute() {
			break
		}
	}
	alice.conn.Write(mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(1, []byte{0xfc})))
	bob.expectNone(mumble.TypeUDPTunnel)
	// Deafened users hear nothing (and are muted).
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Mute: proto.Bool(false)})
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(bob.session), Deaf: proto.Bool(true)})
	for {
		var m pb.UserState
		proto.Unmarshal(alice.expect(mumble.TypeUserState), &m)
		if m.GetSession() == bob.session && m.GetDeaf() {
			if !m.GetMute() {
				t.Fatalf("deaf but not muted: %v", &m)
			}
			break
		}
	}
	alice.conn.Write(mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(2, []byte{0xfc})))
	bob.expectNone(mumble.TypeUDPTunnel)
	// Clients cannot suppress anyone.
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Suppress: proto.Bool(true)})
	su.expectDenied(pb.PermissionDenied_Permission)
}

func TestPrioritySpeakerIsShown(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	alice := dial(t, addr, "alice")
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), PrioritySpeaker: proto.Bool(true)})
	for {
		var m pb.UserState
		proto.Unmarshal(alice.expect(mumble.TypeUserState), &m)
		if m.GetSession() == alice.session && m.GetPrioritySpeaker() {
			break
		}
	}
	// New users see it too.
	c, _ := tryDial(t, addr, "bob")
	for {
		var m pb.UserState
		proto.Unmarshal(c.expect(mumble.TypeUserState), &m)
		if m.GetName() == "alice" {
			if !m.GetPrioritySpeaker() {
				t.Fatalf("alice for bob: %v", &m)
			}
			break
		}
	}
}

func drain(c *testClient) []frame {
	var out []frame
	for len(c.frames) > 0 {
		out = append(out, <-c.frames)
	}
	return out
}

func TestBanOptionsFromNewerClients(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	bad := dial(t, addr, "bad")
	// Certificate only: the address (the test's own) stays allowed.
	su.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(bad.session), Ban: proto.Bool(true),
		BanCertificate: proto.Bool(true), BanIp: proto.Bool(false)})
	bad.expect(mumble.TypeUserRemove)
	dialAs(t, addr, dialOpts{name: "other", cert: clientCert(t)})
	// Neither: nothing happens, as in Murmur.
	guest := dial(t, addr, "guest")
	su.send(mumble.TypeUserRemove, &pb.UserRemove{Session: proto.Uint32(guest.session), Ban: proto.Bool(true),
		BanCertificate: proto.Bool(false), BanIp: proto.Bool(false)})
	guest.expectNone(mumble.TypeUserRemove)
}

func TestBansSurviveARestartAndExpiredOnesGo(t *testing.T) {
	db := testDB(t)
	start := time.Now().UTC().Add(-2 * time.Hour)
	db.SaveBans([]BanEntry{{Hash: "old", Start: start, Duration: time.Hour}, {Hash: "abc", Start: start}})
	s, err := New(Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	if s.banned(netip.MustParseAddr("10.0.0.1"), "abc") == nil {
		t.Fatal("saved ban not loaded")
	}
	s.setBans(0, s.currentBans())
	if got, _ := db.Bans(); len(got) != 1 || got[0].Hash != "abc" {
		t.Fatalf("after saving: %+v", got)
	}
}

func TestSuperUserCannotBeMuted(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(su.session), Mute: proto.Bool(true)})
	su.expectDenied(pb.PermissionDenied_SuperUser)
}

func TestMutingInTemporaryChannelsNeedsAPermanentParent(t *testing.T) {
	// The owner of a temporary channel has Write (and so MuteDeafen) there,
	// but may not mute others unless it may in the parent too (as Murmur).
	addr, _ := adminServer(t)
	bob := dialAs(t, addr, dialOpts{name: "bob", cert: clientCert(t)})
	bob.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(bob.session), UserId: proto.Uint32(0)})
	waitForUserID(t, bob, bob.session)
	tmp := bob.createChannel(0, "Tmp", true)
	carol := dial(t, addr, "carol")
	carol.move(carol.session, tmp)
	bob.waitForMove(carol.session, tmp)
	bob.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(carol.session), Mute: proto.Bool(true)})
	bob.expectDenied(pb.PermissionDenied_TemporaryChannel)
}

func TestRecordingIsShown(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Recording: proto.Bool(true)})
	for {
		var m pb.UserState
		proto.Unmarshal(bob.expect(mumble.TypeUserState), &m)
		if m.GetSession() == alice.session && m.GetRecording() {
			break
		}
	}
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Recording: proto.Bool(true)})
	bob.expectNone(mumble.TypeUserState) // no change, no message
}

func TestTimedBans(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	carol, carolID := registered(t, addr, "carol")
	s.SetModerator(su.session, carolID, true)
	bob := dial(t, addr, "bob")
	guest := dial(t, addr, "guest")
	// Without Ban in root; on staff without Write in root; on SuperUser.
	if err := s.Ban(bob.session, guest.session, "", time.Hour); err == nil {
		t.Fatal("a plain user banned")
	}
	if err := s.Ban(alice.session, carol.session, "", time.Hour); err == nil {
		t.Fatal("a moderator banned a moderator")
	}
	if err := s.Ban(alice.session, su.session, "", time.Hour); err == nil {
		t.Fatal("SuperUser was banned")
	}
	if err := s.Ban(alice.session, 999, "", time.Hour); err == nil {
		t.Fatal("no error for a user who is gone")
	}
	if err := s.Ban(alice.session, guest.session, "spam", time.Hour); err != nil {
		t.Fatal(err)
	}
	var m pb.UserRemove
	proto.Unmarshal(guest.expect(mumble.TypeUserRemove), &m)
	if !m.GetBan() || m.GetReason() != "spam" {
		t.Fatalf("guest got %v", &m)
	}
	if bans := s.currentBans(); len(bans) != 1 || bans[0].Duration != time.Hour || bans[0].Reason != "spam" || !bans[0].Prefix.IsValid() {
		t.Fatalf("bans %+v", bans)
	}
}

func TestWhoseMessagesStaffMayDelete(t *testing.T) {
	s, addr := rolesServer(t)
	su := superUser(t, addr)
	alice, aliceID := registered(t, addr, "alice")
	s.SetModerator(su.session, aliceID, true)
	carol, carolID := registered(t, addr, "carol")
	s.SetModerator(su.session, carolID, true)
	_, daveID := registered(t, addr, "dave")
	s.SetAdmin(su.session, daveID, true)
	bob, bobID := registered(t, addr, "bob")
	guest := dial(t, addr, "guest")
	for _, c := range []struct {
		name                 string
		actor, session, user uint32
	}{
		{"a plain user", bob.session, guest.session, 0},
		{"a moderator on a moderator", alice.session, carol.session, 0},
		{"a moderator on an admin by id", alice.session, 0, daveID},
		{"on SuperUser", alice.session, su.session, 0},
		{"on SuperUser by id", su.session, 0, 0},
		{"a session that is gone", alice.session, 999, 0},
	} {
		if _, _, err := s.MessagesOf(c.actor, c.session, c.user); err == nil {
			t.Errorf("%s: no error", c.name)
		}
	}
	for _, c := range []struct {
		name                 string
		actor, session, user uint32
		registered           bool
		id                   uint32
	}{
		{"a registered user by session", alice.session, bob.session, 0, true, bobID},
		{"a registered user by id", alice.session, 0, bobID, true, bobID},
		{"a guest by session", alice.session, guest.session, 0, false, guest.session},
		{"an admin on a moderator", su.session, carol.session, 0, true, carolID},
	} {
		registered, id, err := s.MessagesOf(c.actor, c.session, c.user)
		if err != nil || registered != c.registered || id != c.id {
			t.Errorf("%s: %v %d %v", c.name, registered, id, err)
		}
	}
}

// A connection that passed the ban checks before a ban was published is
// checked again when it joins.
func TestABanPublishedDuringAdmissionStillApplies(t *testing.T) {
	s, _ := startServerAndListen(t, "127.0.0.1", Config{}, nil)
	banned, other := netip.MustParseAddr("203.0.113.9"), netip.MustParseAddr("198.51.100.1")
	own := [][]byte{[]byte("a Mumble client's own certificate")}
	for _, e := range []struct {
		ban  BanEntry
		host netip.Addr
	}{{BanEntry{Prefix: netip.PrefixFrom(banned, 32)}, banned}, {BanEntry{Hash: "bad"}, other}, {BanEntry{Hash: certHash(own)}, other}} {
		s.setBans(0, []BanEntry{e.ban})
		c := &client{out: make(chan []byte, queueSize), voice: make(chan []byte, queueSize), host: e.host, name: "late", certs: own}
		if reject := s.join(c, identity{name: "late", hash: "bad"}, &pb.CryptSetup{}); reject == nil {
			t.Fatalf("ban %+v: joined", e.ban)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Users) != 0 {
		t.Fatalf("users %v", s.state.Users)
	}
}
