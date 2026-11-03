package mumbled

import (
	"net/netip"
	"sync/atomic"
	"testing"

	"google.golang.org/protobuf/proto"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

func TestInviteOnlyNativeAdmission(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	var closed atomic.Bool
	s, addr := startServerAndListen(t, "127.0.0.1", Config{
		DB: db, AllowGuest: func() bool { return !closed.Load() },
		AccountLogin: func(name, password string) (*WebAccount, bool) {
			if name == "password-user" {
				if password != "correct" {
					return nil, false
				}
				return &WebAccount{Name: name, CertHash: "password-user-cert"}, true
			}
			return nil, true
		},
	}, nil)
	cert := clientCert(t)
	alice := dialAs(t, addr, dialOpts{name: "alice", cert: cert})
	alice.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(alice.session), UserId: proto.Uint32(0)})
	waitForUserID(t, alice, alice.session)
	guest := dialAs(t, addr, dialOpts{name: "guest", cert: clientCert(t)})
	closed.Store(true)
	// Existing accounts and certificate registrations still connect.
	alice.conn.Close()
	dialAs(t, addr, dialOpts{name: "alice", cert: cert})
	superUser(t, addr)
	dialAs(t, addr, dialOpts{name: "password-user", password: "correct"})
	joined(t, webClient(t, s, Origin{Host: netip.MustParseAddr("203.0.113.5"), Account: &WebAccount{Name: "existing-web", CertHash: "web-hash"}}, "existing-web"))
	newGuest, err := tryDialWith(t, addr, dialOpts{name: "newguest", cert: clientCert(t)})
	if err != nil {
		t.Fatal(err)
	}
	var reject pb.Reject
	proto.Unmarshal(newGuest.expect(mumble.TypeReject), &reject)
	if reject.GetType() != pb.Reject_AuthenticatorFail {
		t.Fatalf("guest admitted: %v", &reject)
	}
	// A guest already present cannot self-register after the policy changed.
	guest.send(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(guest.session), UserId: proto.Uint32(0)})
	guest.expect(mumble.TypePermissionDenied)
	closed.Store(false)
	dial(t, addr, "openagain")
}
