package mumbled

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

// started is a database as after a first start: the lobby with its
// General, the roles, and the text channels' mirrors (3 under 2).
func started(t *testing.T) *DB {
	t.Helper()
	db := testDB(t)
	for _, f := range []func() error{db.InitLobby, db.InitRoles} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []*Channel{
		{ID: 2, Parent: 0, Name: "Text channels", Position: 1000},
		{ID: 3, Parent: 2, Name: "#general"},
	} {
		if err := db.SaveChannel(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetTextParent(2); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveNextChannel(3); err != nil {
		t.Fatal(err)
	}
	return db
}

func importData() Import {
	admins := newGroup("admin")
	admins.Add[1] = true
	return Import{
		Channels: []Channel{
			{ID: 1, Parent: 0, Name: "Murmur", ACLs: []ACL{{ApplyHere: true, ApplySubs: true, Group: "admin", Allow: Write}}, Groups: map[string]*Group{"admin": admins}},
			{ID: 2, Parent: 1, Name: "Lounge", Description: "sit", Position: 3, Links: map[uint32]bool{3: true}},
			{ID: 3, Parent: 1, Name: "Games", NoInheritACL: true, Links: map[uint32]bool{2: true}},
		},
		Users: []ImportUser{
			{ID: 1, Name: "Alice", CertHash: "aa", Comment: "hi", LastChannel: 2},
			{ID: 2, Name: "Bob", CertHash: "bb"},
		},
		Bans: []BanEntry{{Prefix: netip.MustParsePrefix("10.0.0.0/8"), Reason: "spam", Start: time.Unix(1000, 0), Duration: time.Hour}},
	}
}

func byName(t *testing.T, db *DB) map[string]Channel {
	t.Helper()
	cs, err := db.Channels()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Channel{}
	for _, c := range cs {
		out[c.Name] = c
	}
	return out
}

func TestImportAfterTheFirstStart(t *testing.T) {
	db := started(t)
	if err := db.Import(importData()); err != nil {
		t.Fatal(err)
	}
	cs := byName(t, db)
	if _, ok := cs["General"]; ok {
		t.Error("the automatic General stays")
	}
	if cs["#general"].ID != 3 || cs["#general"].Parent != 2 {
		t.Errorf("mirror moved: %+v", cs["#general"])
	}
	top, lounge, games := cs["Murmur"], cs["Lounge"], cs["Games"]
	if top.ID <= 3 || top.Parent != 0 || lounge.Parent != top.ID || games.Parent != top.ID {
		t.Fatalf("tree %+v %+v %+v", top, lounge, games)
	}
	if lounge.Description != "sit" || lounge.Position != 3 || !games.NoInheritACL || !lounge.Links[games.ID] {
		t.Errorf("channel data %+v %+v", lounge, games)
	}
	if len(top.ACLs) != 1 || !top.Groups["admin"].Add[1] {
		t.Errorf("acl/groups %+v %+v", top.ACLs, top.Groups)
	}
	if next, _ := db.NextChannel(); next < games.ID {
		t.Errorf("next channel %d", next)
	}
	alice, err := db.UserByID(1)
	if err != nil || alice == nil || alice.Name != "Alice" || alice.CertHash != "aa" || alice.LastChannel != lounge.ID {
		t.Fatalf("alice %+v %v", alice, err)
	}
	if c, _, _ := db.UserContent(1); c != "hi" {
		t.Errorf("comment %q", c)
	}
	if bans, _ := db.Bans(); len(bans) != 1 || bans[0].Reason != "spam" {
		t.Errorf("bans %+v", bans)
	}
	// Kallisti's root stays (the lobby, the roles).
	root := NewState("Root", mustChannels(t, db)).Channels[0]
	if root.Groups["moderator"] == nil {
		t.Error("roles gone")
	}
}

func mustChannels(t *testing.T, db *DB) []Channel {
	t.Helper()
	cs, err := db.Channels()
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// One server as the whole server: its root is Kallisti's root, no lobby
// is added, and the moderator role comes back at the next start.
func TestImportWithTheServersRoot(t *testing.T) {
	db := testDB(t) // before the first start
	imp := importData()
	imp.Root = &Channel{ACLs: []ACL{{ApplyHere: true, ApplySubs: true, Group: "admin", Allow: Write}}, Groups: map[string]*Group{"admin": newGroup("admin")}, Description: "welcome"}
	imp.ServerName = "Old server"
	imp.Channels = imp.Channels[1:]
	imp.Channels[0].Parent, imp.Channels[1].Parent = 0, 0
	if err := db.Import(imp); err != nil {
		t.Fatal(err)
	}
	for _, f := range []func() error{db.InitLobby, db.InitRoles} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	cs := byName(t, db)
	if _, ok := cs["General"]; ok {
		t.Error("a General was added")
	}
	root := NewState("Root", mustChannels(t, db)).Channels[0]
	if root.Description != "welcome" || root.Groups["moderator"] == nil || root.Groups["admin"] == nil {
		t.Errorf("root %+v", root)
	}
	for _, a := range root.ACLs {
		if a.Deny&Speak != 0 && a.Group == "all" {
			t.Error("lobby added")
		}
	}
	if name, _ := db.ServerName(); name != "Old server" {
		t.Errorf("name %q", name)
	}
}

func TestImportRefusesAUsedServer(t *testing.T) {
	db := started(t)
	if _, err := db.RegisterUser("carol", "cc"); err != nil {
		t.Fatal(err)
	}
	if err := db.Import(importData()); err == nil || !strings.Contains(err.Error(), "registered") {
		t.Fatalf("users: %v", err)
	}
	db = started(t)
	if err := db.SaveChannel(&Channel{ID: 9, Parent: 0, Name: "Own"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Import(importData()); err == nil || !strings.Contains(err.Error(), "Own") {
		t.Fatalf("channels: %v", err)
	}
}
