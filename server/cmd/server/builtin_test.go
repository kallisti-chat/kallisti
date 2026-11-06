package main

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"io"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/bridge"
	"kallisti.chat/server/internal/certs"
	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/password"
	"kallisti.chat/server/internal/store"
)

func TestWebAccountsJoinTheBuiltInServer(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	certPEM, keyPEM, _ := certs.Generate("alice")
	db.CreateAccount(store.Account{Name: "alice", PasswordHash: "x", CertPEM: certPEM, KeyPEM: keyPEM}, time.Now(), "")
	b, err := startBuiltin(&config.Builtin{Listen: "127.0.0.1:0", ServerName: "Test", MaxUsers: 10, CertDir: t.TempDir()}, db, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	cert, _ := certs.Load(certPEM, keyPEM)
	conn, err := b.dial(context.Background(), bridge.Upstream{IP: "203.0.113.5", AccountName: "alice", Cert: &cert})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	v1 := uint32(1<<16 | 5<<8)
	v2 := uint64(1<<48 | 5<<32)
	go conn.Write(append(mumble.MessageFrame(mumble.TypeVersion, &pb.Version{VersionV1: &v1, VersionV2: &v2}),
		mumble.MessageFrame(mumble.TypeAuthenticate, &pb.Authenticate{Username: proto.String("alice")})...))
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	header := make([]byte, 6)
	for {
		if _, err := io.ReadFull(conn, header); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, binary.BigEndian.Uint32(header[2:]))
		io.ReadFull(conn, body)
		if binary.BigEndian.Uint16(header) == mumble.TypeServerSync {
			break
		}
	}
	sum := sha1.Sum(cert.Certificate[0])
	r, _ := b.db.UserByHash(hex.EncodeToString(sum[:]))
	if r == nil || r.Name != "alice" {
		t.Fatalf("registration %+v", r)
	}
	// The account's name is not free for others, and Mumble names are not
	// free for new accounts.
	if !b.nameTaken("alice") || b.nameTaken("nobody") {
		t.Fatal("nameTaken")
	}
}

func TestAccountLoginChecksTheAccountPassword(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	certPEM, keyPEM, _ := certs.Generate("alice")
	hash, _ := password.Hash("secret pw")
	alice, _ := db.CreateAccount(store.Account{Name: "Alice", PasswordHash: hash, CertPEM: certPEM, KeyPEM: keyPEM}, time.Now(), "")
	db.CreateAccount(store.Account{Name: "gus", CertPEM: certPEM, KeyPEM: keyPEM, Temporary: true}, time.Now(), "")
	login := accountLogin(db)
	if a, ok := login("alice", "secret pw"); !ok || a == nil || a.Name != "Alice" || a.CertHash != accountCertHash(alice) {
		t.Fatalf("right password: %+v %v", a, ok)
	}
	if a, ok := login("Alice", "wrong"); ok || a != nil {
		t.Fatalf("wrong password: %+v %v", a, ok)
	}
	// Guest accounts and unknown names: no account to log in to.
	for _, name := range []string{"gus", "nobody"} {
		if a, ok := login(name, "secret pw"); !ok || a != nil {
			t.Fatalf("%s: %+v %v", name, a, ok)
		}
	}
}

func TestBuiltinUsesSavedRegistrationPolicy(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	certPEM, keyPEM, err := certs.Generate("alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAccount(store.Account{Name: "alice", CertPEM: certPEM, KeyPEM: keyPEM}, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	cert, err := certs.Load(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	b, err := startBuiltin(&config.Builtin{Listen: "127.0.0.1:0", ServerName: "Test", CertDir: t.TempDir()}, db, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	join := func(name string, account bool, wantReject bool) {
		t.Helper()
		up := bridge.Upstream{IP: "203.0.113.9"}
		if account {
			up.AccountName, up.Cert = name, &cert
		}
		conn, err := b.dial(context.Background(), up)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		go conn.Write(append(mumble.MessageFrame(mumble.TypeVersion, &pb.Version{VersionV2: proto.Uint64(1<<48 | 5<<32)}),
			mumble.MessageFrame(mumble.TypeAuthenticate, &pb.Authenticate{Username: proto.String(name), Opus: proto.Bool(true)})...))
		for {
			var header [6]byte
			if _, err := io.ReadFull(conn, header[:]); err != nil {
				t.Fatal(err)
			}
			body := make([]byte, binary.BigEndian.Uint32(header[2:]))
			if _, err := io.ReadFull(conn, body); err != nil {
				t.Fatal(err)
			}
			typ := binary.BigEndian.Uint16(header[:2])
			if typ == mumble.TypeReject || typ == mumble.TypeServerSync {
				if (typ == mumble.TypeReject) != wantReject {
					t.Fatalf("%s: response %d, want reject %v", name, typ, wantReject)
				}
				return
			}
		}
	}
	join("before", false, false)
	if err := db.SetInviteOnly(true); err != nil {
		t.Fatal(err)
	}
	join("blocked", false, true)
	join("alice", true, false)
	if err := db.SetInviteOnly(false); err != nil {
		t.Fatal(err)
	}
	join("after", false, false)
}

// An admin's deletion takes the account with the registration's name and
// certificate only; a database error stops it.
func TestDeleteWebAccount(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, _ := certs.Generate("alice")
	alice, _ := db.CreateAccount(store.Account{Name: "alice", PasswordHash: "x", CertPEM: certPEM, KeyPEM: keyPEM}, time.Now(), "")
	var deleted []string
	del := deleteWebAccount(db, func(a store.Account) error { deleted = append(deleted, a.Name); return nil })
	for _, c := range []struct{ name, hash string }{{"bob", "b"}, {"alice", "another"}, {"Alice", accountCertHash(alice)}} {
		if err := del(c.name, c.hash); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(deleted, []string{"alice"}) {
		t.Fatalf("deleted %v", deleted)
	}
	db.Close()
	if err := del("alice", accountCertHash(alice)); err == nil {
		t.Error("a database error deleted nothing, silently")
	}
}

// A push goes to the account's own registration (by certificate), not to a
// Mumble user with its name.
func TestPushUser(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mdb, err := mumbled.OpenDB(db.DB())
	if err != nil {
		t.Fatal(err)
	}
	account := func(name string) store.Account {
		certPEM, keyPEM, _ := certs.Generate(name)
		acc, _ := db.CreateAccount(store.Account{Name: name, PasswordHash: "x", CertPEM: certPEM, KeyPEM: keyPEM}, time.Now(), "")
		return acc
	}
	alice, _ := mdb.RegisterUser("alice", accountCertHash(account("alice")))
	account("bob")
	mdb.RegisterUser("bob", "a Mumble client's")
	mdb.RegisterUser("carol2", accountCertHash(account("carol"))) // renamed by an admin: no login
	user := pushUser(db, mdb)
	if id, ok := user("Alice"); !ok || id != alice {
		t.Errorf("alice: %d %v", id, ok)
	}
	for _, name := range []string{"bob", "carol", "nobody"} {
		if id, ok := user(name); ok {
			t.Errorf("%s: %d", name, id)
		}
	}
}
