package mumbled

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1) // one in-memory database
	t.Cleanup(func() { sqlDB.Close() })
	db, err := OpenDB(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// setAdminPassword gives SuperUser a password with a cheap argon2id hash
// (Check takes the parameters from it): the real one takes 0.3 s under
// -race and seconds on a busy machine, longer than expect waits.
func setAdminPassword(db *DB, pw string) {
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte(pw), salt, 1, 64, 1, 32)
	b64 := base64.RawStdEncoding
	db.SetPassword(0, fmt.Sprintf("$argon2id$v=%d$m=64,t=1,p=1$%s$%s", argon2.Version, b64.EncodeToString(salt), b64.EncodeToString(key)))
}

func TestChannelsAreSavedWithLinks(t *testing.T) {
	db := testDB(t)
	s := NewState("Root", nil)
	a, _ := s.AddChannel(0, "A", false)
	b, _ := s.AddChannel(a.ID, "B", false)
	b.Description, b.Position = "about B", 3
	s.Link(a.ID, b.ID)
	for _, c := range []*Channel{a, b} {
		if err := db.SaveChannel(c); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := db.Channels()
	if err != nil {
		t.Fatal(err)
	}
	loaded := NewState("Root", saved)
	got := loaded.Channels[b.ID]
	if got == nil || got.Parent != a.ID || got.Description != "about B" || got.Position != 3 || !got.Links[a.ID] {
		t.Fatalf("loaded %+v", got)
	}
	// New channels do not reuse ids.
	c, _ := loaded.AddChannel(0, "C", false)
	if c.ID <= b.ID {
		t.Fatalf("id %d reused", c.ID)
	}
	if err := db.DeleteChannels([]uint32{a.ID}); err != nil {
		t.Fatal(err)
	}
	saved, _ = db.Channels()
	if len(saved) != 2 || len(NewState("Root", saved).Channels[b.ID].Links) != 0 {
		t.Fatalf("after delete: %+v", saved)
	}
}

func TestLinksToRootAndIDsSurviveRestarts(t *testing.T) {
	db := testDB(t)
	saved, _ := db.Channels()
	s := NewState("Root", saved)
	s.nextChannel = db.mustNext(t)
	a, _ := s.AddChannel(0, "A", false)
	b, _ := s.AddChannel(0, "B", false)
	s.Link(a.ID, 0)
	db.SaveChannel(a)
	db.SaveChannel(b)
	db.SaveNextChannel(s.nextChannel)
	if err := db.DeleteChannels([]uint32{b.ID}); err != nil {
		t.Fatal(err)
	}
	saved, _ = db.Channels()
	s2 := NewState("Root", saved)
	s2.nextChannel = db.mustNext(t)
	if !s2.Channels[a.ID].Links[0] {
		t.Fatal("link to root lost")
	}
	c, _ := s2.AddChannel(0, "C", false)
	if c.ID <= b.ID {
		t.Fatalf("id %d of a deleted channel reused", c.ID)
	}
}

func (d *DB) mustNext(t *testing.T) uint32 {
	t.Helper()
	n, err := d.NextChannel()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRegisteredUsers(t *testing.T) {
	db := testDB(t)
	id, err := db.RegisterUser("Alice", "aaa")
	if err != nil || id == 0 {
		t.Fatalf("id %d err %v", id, err)
	}
	if _, err := db.RegisterUser("alice", "bbb"); err == nil {
		t.Fatal("registered a taken name")
	}
	byName, _ := db.UserByName("ALICE")
	byHash, _ := db.UserByHash("aaa")
	if byName == nil || byHash == nil || byName.ID != id || byHash.Name != "Alice" {
		t.Fatalf("by name %+v by hash %+v", byName, byHash)
	}
	if u, _ := db.UserByHash(""); u != nil {
		t.Fatal("found a user by an empty hash")
	}
	db.SetLastChannel(id, 7)
	byName, _ = db.UserByName("alice")
	if byName.LastChannel != 7 {
		t.Fatalf("last channel %d", byName.LastChannel)
	}
	// SuperUser exists from the start, with a password set by the admin.
	if err := db.SetPassword(0, "hash:pw"); err != nil {
		t.Fatal(err)
	}
	super, _ := db.UserByName("superuser")
	if super == nil || super.ID != 0 || super.PasswordHash != "hash:pw" {
		t.Fatalf("superuser %+v", super)
	}
	got, _ := db.UserNames([]uint32{0, id, 99})
	if got[id] != "Alice" || got[0] != "SuperUser" || len(got) != 2 {
		t.Fatalf("names %v", got)
	}
}

func TestNewDatabasesHaveTheDefaultACL(t *testing.T) {
	db := testDB(t)
	saved, _ := db.Channels()
	s := NewState("Root", saved)
	acls, groups := DefaultRootACL()
	if len(s.Channels[0].ACLs) != len(acls) || s.Channels[0].Groups["admin"] == nil || len(groups) != 2 || s.Channels[0].Groups["moderator"] == nil {
		t.Fatalf("root ACL %+v groups %+v", s.Channels[0].ACLs, s.Channels[0].Groups)
	}
	// An admin who removes all root ACLs keeps it that way after a restart.
	s.Channels[0].ACLs = nil
	db.SaveChannel(s.Channels[0])
	db2, err := OpenDB(db.db)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ = db2.Channels()
	if len(NewState("Root", saved).Channels[0].ACLs) != 0 {
		t.Fatal("default ACL came back")
	}
}

func TestACLsAndGroupsAreSaved(t *testing.T) {
	db := testDB(t)
	s := NewState("Root", nil)
	a, _ := s.AddChannel(0, "A", false)
	a.NoInheritACL = true
	a.ACLs = []ACL{
		{ApplyHere: true, ApplySubs: false, Group: "all", Deny: Enter},
		{ApplyHere: true, ApplySubs: true, HasUser: true, UserID: 5, Allow: Enter | Speak},
	}
	g := newGroup("mods")
	g.Inheritable = false
	g.Add[5], g.Remove[6] = true, true
	a.Groups = map[string]*Group{"mods": g}
	if err := db.SaveChannel(a); err != nil {
		t.Fatal(err)
	}
	saved, _ := db.Channels()
	got := NewState("Root", saved).Channels[a.ID]
	if !got.NoInheritACL || len(got.ACLs) != 2 || got.ACLs[0] != a.ACLs[0] || got.ACLs[1] != a.ACLs[1] {
		t.Fatalf("ACLs %+v", got.ACLs)
	}
	mods := got.Groups["mods"]
	if mods == nil || mods.Inheritable || !mods.Inherit || !mods.Add[5] || !mods.Remove[6] {
		t.Fatalf("group %+v", mods)
	}
	db.DeleteChannels([]uint32{a.ID})
	var n int
	db.db.QueryRow(`SELECT (SELECT COUNT(*) FROM mumble_acl WHERE channel_id = ?) + (SELECT COUNT(*) FROM mumble_groups WHERE channel_id = ?)`, a.ID, a.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("%d ACL rows left", n)
	}
}

func TestOldDatabasesGetTheNewColumn(t *testing.T) {
	sqlDB, _ := sql.Open("sqlite", ":memory:")
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	sqlDB.Exec(`CREATE TABLE mumble_channels (id INTEGER PRIMARY KEY, parent INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '', position INTEGER NOT NULL DEFAULT 0)`)
	if _, err := OpenDB(sqlDB); err != nil {
		t.Fatal(err)
	}
}

func TestRenameAndUnregister(t *testing.T) {
	db := testDB(t)
	a, _ := db.RegisterUser("Alice", "aaa")
	b, _ := db.RegisterUser("Bob", "bbb")
	if err := db.RenameUser(a, "bob"); err == nil {
		t.Fatal("renamed to a taken name")
	}
	if err := db.RenameUser(a, "Alicia"); err != nil {
		t.Fatal(err)
	}
	if err := db.UnregisterUser(0); err == nil {
		t.Fatal("unregistered SuperUser")
	}
	if err := db.UnregisterUser(b); err != nil {
		t.Fatal(err)
	}
	users, _ := db.Users()
	if len(users) != 1 || users[0].Name != "Alicia" {
		t.Fatalf("users %+v", users)
	}
	if u, _ := db.UserByHash("bbb"); u != nil {
		t.Fatal("unregistered user still found")
	}
}

func TestBansAreSaved(t *testing.T) {
	db := testDB(t)
	start := time.Unix(1_800_000_000, 0).UTC()
	bans := []BanEntry{
		{Prefix: netip.MustParsePrefix("10.0.0.0/8"), Name: "x", Reason: "spam", Start: start, Duration: time.Hour},
		{Hash: "abc", Start: start},
	}
	if err := db.SaveBans(bans); err != nil {
		t.Fatal(err)
	}
	got, err := db.Bans()
	if err != nil || len(got) != 2 || got[0] != bans[0] || got[1] != bans[1] {
		t.Fatalf("bans %+v err %v", got, err)
	}
	db.SaveBans(nil)
	if got, _ := db.Bans(); len(got) != 0 {
		t.Fatal("bans not replaced")
	}
}

func TestTheLobbyIsSetUpOnce(t *testing.T) {
	db := testDB(t)
	if err := db.InitLobby(); err != nil {
		t.Fatal(err)
	}
	saved, _ := db.Channels()
	s := NewState("Root", saved)
	guest := &User{Session: 1, Name: "guest"}
	if s.Has(guest, 0, Speak) {
		t.Fatal("voice in the lobby")
	}
	var general *Channel
	for _, c := range s.Channels {
		if c.Name == "General" {
			general = c
		}
	}
	if general == nil || !s.Has(guest, general.ID, Speak) {
		t.Fatalf("no General voice channel: %+v", s.Channels)
	}
	// Again: nothing changes (an admin may have removed General since).
	db.DeleteChannels([]uint32{general.ID})
	if err := db.InitLobby(); err != nil {
		t.Fatal(err)
	}
	saved, _ = db.Channels()
	acls, _ := DefaultRootACL()
	if len(saved) != 1 || len(NewState("Root", saved).Channels[0].ACLs) != len(acls)+1 {
		t.Fatalf("after a second run: %+v", saved)
	}
}

func TestAnOldServerKeepsItsChannels(t *testing.T) {
	db := testDB(t)
	s := NewState("Root", nil)
	games, _ := s.AddChannel(0, "Games", false)
	if err := db.SaveChannel(games); err != nil {
		t.Fatal(err)
	}
	if err := db.InitLobby(); err != nil {
		t.Fatal(err)
	}
	saved, _ := db.Channels()
	for _, c := range saved {
		if c.Name == "General" {
			t.Fatal("made General although channels exist")
		}
	}
}

func TestCheckPassword(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "adminpw")
	if name, ok := db.CheckPassword("superuser", "adminpw"); !ok || name != "SuperUser" {
		t.Fatalf("superuser: %q %v", name, ok)
	}
	if _, ok := db.CheckPassword("SuperUser", "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	// Registrations without a password (certificate only) and unknown names.
	db.RegisterUser("alice", "aaa")
	for _, n := range []string{"alice", "nobody"} {
		if _, ok := db.CheckPassword(n, ""); ok {
			t.Fatalf("%s accepted", n)
		}
	}
}

func TestWebPasswordChecksShareTheLockout(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "adminpw")
	s, _ := startServerAndListen(t, "127.0.0.1", Config{DB: db}, nil)
	ip, other := netip.MustParseAddr("203.0.113.5"), netip.MustParseAddr("203.0.113.6")
	for range maxLoginFailures {
		s.CheckPassword(ip, "SuperUser", "guess")
	}
	if _, ok := s.CheckPassword(ip, "SuperUser", "adminpw"); ok {
		t.Fatal("not locked out")
	}
	if !s.logins.blocked(ip) {
		t.Fatal("native logins not blocked")
	}
	if name, ok := s.CheckPassword(other, "superuser", "adminpw"); !ok || name != "SuperUser" {
		t.Fatalf("other IP: %q %v", name, ok)
	}
}

func TestUserIDsAreNeverReused(t *testing.T) {
	db := testDB(t)
	a, _ := db.RegisterUser("a", "ha")
	b, _ := db.RegisterUser("b", "hb")
	if err := db.UnregisterUser(b); err != nil {
		t.Fatal(err)
	}
	c, err := db.RegisterUser("c", "hc")
	if err != nil || c <= b || a >= b {
		t.Fatalf("ids %d %d %d (%v)", a, b, c, err)
	}
}

// A claimed registration moves to its account's certificate (once, and only
// as it was when the link was made); its own password goes.
func TestMoveUser(t *testing.T) {
	db := testDB(t)
	id, err := db.RegisterUser("feld", "old")
	if err != nil {
		t.Fatal(err)
	}
	db.db.Exec(`UPDATE mumble_users SET password_hash = 'x' WHERE id = ?`, id)
	tx, _ := db.db.Begin()
	if err := MoveUser(tx, id, "feld", "old", "new"); err != nil {
		t.Fatal(err)
	}
	if err := MoveUser(tx, id, "feld", "old", "newer"); !errors.Is(err, ErrUserChanged) {
		t.Fatalf("second move: %v", err)
	}
	tx.Commit()
	if r, _ := db.UserByHash("new"); r == nil || r.ID != id || r.PasswordHash != "" {
		t.Fatalf("moved: %+v", r)
	}
	// The old certificate stays the user's too (its Mumble app keeps working).
	if r, _ := db.UserByHash("old"); r == nil || r.ID != id || r.CertHash != "new" {
		t.Fatalf("old certificate: %+v", r)
	}
}

// Further certificates end with the account's logins (DropOtherCerts) and
// with the user.
func TestOtherCertsEnd(t *testing.T) {
	db := testDB(t)
	claimed := func(name, old, account string) uint32 {
		t.Helper()
		id, err := db.RegisterUser(name, old)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := MoveUser(tx, id, name, old, account); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := claimed("feld", "old", "new")
	if err := db.DropOtherCerts("new"); err != nil {
		t.Fatal(err)
	}
	if r, _ := db.UserByHash("old"); r != nil {
		t.Fatalf("old certificate after the drop: %+v", r)
	}
	if r, _ := db.UserByHash("new"); r == nil || r.ID != id {
		t.Fatalf("the account's own: %+v", r)
	}

	// A further certificate is nobody else's own.
	claimed("nora", "n1", "n2")
	if _, err := db.RegisterUser("guest", "n1"); err == nil {
		t.Fatal("a registration with another user's further certificate")
	}

	other := claimed("mira", "m1", "m2")
	if err := db.UnregisterUser(other); err != nil {
		t.Fatal(err)
	}
	if r, _ := db.UserByHash("m1"); r != nil {
		t.Fatalf("certificate of a deleted user: %+v", r)
	}
}
