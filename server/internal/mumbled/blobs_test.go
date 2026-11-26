package mumbled

import (
	"bytes"
	"crypto/sha1"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// waitForState reads UserStates about a session until one passes ok.
func (c *testClient) waitForState(session uint32, ok func(*pb.UserState) bool) *pb.UserState {
	c.t.Helper()
	for {
		var m pb.UserState
		proto.Unmarshal(c.expect(mumble.TypeUserState), &m)
		if m.GetSession() == session && ok(&m) {
			return &m
		}
	}
}

func TestShortCommentsGoOutAsTheyAre(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Comment: proto.String("hi <b>there</b>")})
	m := bob.waitForState(alice.session, func(m *pb.UserState) bool { return m.Comment != nil })
	if m.GetComment() != "hi there" {
		t.Fatalf("comment %q", m.GetComment())
	}
}

func TestLongCommentsAndAvatarsGoOutAsHashes(t *testing.T) {
	addr, _ := adminServer(t)
	alice, _ := registered(t, addr, "alice")
	bob := dial(t, addr, "bob")
	comment := strings.Repeat("long comment ", 20)
	texture := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 100)
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Comment: proto.String(comment), Texture: texture})
	m := bob.waitForState(alice.session, func(m *pb.UserState) bool { return m.CommentHash != nil })
	wantC, wantT := sha1.Sum([]byte(strings.TrimSpace(comment))), sha1.Sum(texture)
	if !bytes.Equal(m.CommentHash, wantC[:]) || !bytes.Equal(m.TextureHash, wantT[:]) || m.Comment != nil || m.Texture != nil {
		t.Fatalf("state %v", m)
	}
	// The full data on request.
	bob.send(mumble.TypeRequestBlob, &pb.RequestBlob{SessionComment: []uint32{alice.session}, SessionTexture: []uint32{alice.session}})
	gotComment, gotTexture := false, false
	for !gotComment || !gotTexture {
		m := bob.waitForState(alice.session, func(m *pb.UserState) bool { return m.Comment != nil || m.Texture != nil })
		gotComment = gotComment || m.GetComment() == strings.TrimSpace(comment)
		gotTexture = gotTexture || bytes.Equal(m.Texture, texture)
	}
	// New users get the hashes.
	c, _ := tryDial(t, addr, "carol")
	m = c.waitForState(alice.session, func(*pb.UserState) bool { return true })
	if !bytes.Equal(m.CommentHash, wantC[:]) || !bytes.Equal(m.TextureHash, wantT[:]) {
		t.Fatalf("for carol: %v", m)
	}
}

func TestLongDescriptionsGoOutAsHashes(t *testing.T) {
	addr := startOpen(t)
	alice := dial(t, addr, "alice")
	games := alice.createChannel(0, "Games", false)
	desc := strings.Repeat("all about games ", 20)
	alice.send(mumble.TypeChannelState, &pb.ChannelState{ChannelId: proto.Uint32(games), Description: proto.String(desc)})
	for {
		var m pb.ChannelState
		proto.Unmarshal(alice.expect(mumble.TypeChannelState), &m)
		if m.GetChannelId() == games && m.DescriptionHash != nil {
			break
		}
	}
	bob, _ := tryDial(t, addr, "bob")
	for {
		var m pb.ChannelState
		proto.Unmarshal(bob.expect(mumble.TypeChannelState), &m)
		if m.GetChannelId() == games {
			if m.DescriptionHash == nil || m.Description != nil {
				t.Fatalf("sync %v", &m)
			}
			break
		}
	}
	bob.expect(mumble.TypeServerSync)
	bob.send(mumble.TypeRequestBlob, &pb.RequestBlob{ChannelDescription: []uint32{games}})
	for {
		var m pb.ChannelState
		proto.Unmarshal(bob.expect(mumble.TypeChannelState), &m)
		if m.GetChannelId() == games && m.Description != nil {
			if m.GetDescription() != strings.TrimSpace(desc) {
				t.Fatalf("description %q", m.GetDescription())
			}
			break
		}
	}
}

func TestOthersCommentsCanOnlyBeCleared(t *testing.T) {
	addr, _ := adminServer(t)
	su := superUser(t, addr)
	alice := dial(t, addr, "alice")
	bob := dial(t, addr, "bob")
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Comment: proto.String("rude")})
	bob.waitForState(alice.session, func(m *pb.UserState) bool { return m.Comment != nil })
	bob.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Comment: proto.String("")})
	bob.expectDenied(pb.PermissionDenied_Permission) // needs ResetUserContent
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Comment: proto.String("nicer")})
	su.expectDenied(pb.PermissionDenied_TextTooLong) // only clearing
	su.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Comment: proto.String("")})
	m := bob.waitForState(alice.session, func(m *pb.UserState) bool { return m.Comment != nil })
	if m.GetComment() != "" {
		t.Fatalf("comment %q", m.GetComment())
	}
}

func TestRegisteredUsersKeepCommentAndAvatar(t *testing.T) {
	addr, _ := adminServer(t)
	cert := clientCert(t)
	alice := dialAs(t, addr, dialOpts{name: "alice", cert: cert})
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), UserId: proto.Uint32(0)})
	waitForUserID(t, alice, alice.session)
	texture := bytes.Repeat([]byte{7}, 300)
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Comment: proto.String("still me"), Texture: texture})
	alice.waitForState(alice.session, func(m *pb.UserState) bool { return m.Comment != nil })
	alice.conn.Close()
	again, _ := tryDialWith(t, addr, dialOpts{name: "alice", cert: cert})
	for {
		var m pb.UserState
		proto.Unmarshal(again.expect(mumble.TypeUserState), &m)
		if m.GetName() == "alice" {
			want := sha1.Sum(texture)
			if m.GetComment() != "still me" || !bytes.Equal(m.TextureHash, want[:]) {
				t.Fatalf("after reconnect: %v", &m)
			}
			return
		}
	}
}

func TestRequestBlobAnswersEachIDOnce(t *testing.T) {
	addr, _ := adminServer(t)
	alice, _ := registered(t, addr, "alice")
	bob := dial(t, addr, "bob")
	texture := bytes.Repeat([]byte{1}, 500)
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Texture: texture})
	bob.waitForState(alice.session, func(m *pb.UserState) bool { return m.TextureHash != nil })
	ids := make([]uint32, 100)
	for i := range ids {
		ids[i] = alice.session
	}
	bob.send(mumble.TypeRequestBlob, &pb.RequestBlob{SessionTexture: ids})
	bob.waitForState(alice.session, func(m *pb.UserState) bool { return m.Texture != nil })
	bob.expectNone(mumble.TypeUserState)
}

func TestTooLargeAvatarsAreRefused(t *testing.T) {
	addr, _ := adminServer(t)
	alice, _ := registered(t, addr, "alice")
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Texture: make([]byte, maxTexture+1)})
	alice.expectDenied(pb.PermissionDenied_TextTooLong)
}

func TestGuestsCannotSetAnAvatar(t *testing.T) {
	addr := startOpen(t)
	guest := dial(t, addr, "guest")
	guest.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(guest.session), Texture: []byte{1, 2, 3}})
	guest.expectDenied(pb.PermissionDenied_Text)
	// Clearing is no change, and allowed.
	guest.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(guest.session), Texture: []byte{}})
	guest.expectNone(mumble.TypePermissionDenied)
}

func TestACommentSetBeforeRegisteringIsKept(t *testing.T) {
	addr, db := adminServer(t)
	cert := clientCert(t)
	alice := dialAs(t, addr, dialOpts{name: "alice", cert: cert})
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), Comment: proto.String("before")})
	alice.waitForState(alice.session, func(m *pb.UserState) bool { return m.Comment != nil })
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), UserId: proto.Uint32(0)})
	id := waitForUserID(t, alice, alice.session)
	// The comment is saved just after the server sends the new id.
	eventually(t, func() bool { c, _, err := db.UserContent(id); return err == nil && c == "before" })
	alice.conn.Close()
	again, _ := tryDialWith(t, addr, dialOpts{name: "alice", cert: cert})
	for {
		var m pb.UserState
		proto.Unmarshal(again.expect(mumble.TypeUserState), &m)
		if m.GetName() == "alice" {
			if m.GetComment() != "before" {
				t.Fatalf("comment %q", m.GetComment())
			}
			return
		}
	}
}
