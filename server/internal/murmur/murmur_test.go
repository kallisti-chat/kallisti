package murmur

import (
	"database/sql"
	"net/netip"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"kallisti.chat/server/internal/mumbled"
)

// Murmur's schema (version 9), as mumble-server 1.4 and 1.5 write it.
const schema = "CREATE TABLE `servers` (`server_id` INTEGER PRIMARY KEY AUTOINCREMENT);" +
	"CREATE TABLE `channels` (`server_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `parent_id` INTEGER, `name` TEXT, `inheritacl` INTEGER);" +
	"CREATE TABLE `channel_info` (`server_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `key` INTEGER, `value` TEXT);" +
	"CREATE TABLE `channel_links` (`server_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `link_id` INTEGER NOT NULL);" +
	"CREATE TABLE `acl` (`server_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `priority` INTEGER, `user_id` INTEGER, `group_name` TEXT, `apply_here` INTEGER, `apply_sub` INTEGER, `grantpriv` INTEGER, `revokepriv` INTEGER);" +
	"CREATE TABLE `groups` (`group_id` INTEGER PRIMARY KEY AUTOINCREMENT, `server_id` INTEGER NOT NULL, `name` TEXT, `channel_id` INTEGER NOT NULL, `inherit` INTEGER, `inheritable` INTEGER);" +
	"CREATE TABLE `group_members` (`group_id` INTEGER NOT NULL, `server_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `addit` INTEGER);" +
	"CREATE TABLE `users` (`server_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `name` TEXT NOT NULL, `pw` TEXT, `salt` TEXT, `kdfiterations` INTEGER, `lastchannel` INTEGER, `texture` BLOB, `last_active` DATE, `last_disconnect` DATE);" +
	"CREATE TABLE `user_info` (`server_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `key` INTEGER, `value` TEXT);" +
	"CREATE TABLE `bans` (`server_id` INTEGER NOT NULL, `base` BLOB, `mask` INTEGER, `name` TEXT, `hash` TEXT, `reason` TEXT, `start` DATE, `duration` INTEGER);" +
	"CREATE TABLE `config` (`server_id` INTEGER NOT NULL, `key` TEXT, `value` TEXT);" +
	"CREATE TABLE `channel_listeners` (`server_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `channel_id` INTEGER NOT NULL, `volume_adjustment` FLOAT DEFAULT 1, `enabled` SMALLINT DEFAULT 1);"

// Two servers. 1 "Main": Lobby (1) with Games (2) under it, linked to
// Music (3); root ACL for @admin, a group with alice; alice (cert aa,
// email, comment), bob (cert bb, a password), carol (no cert: password
// only); an IPv4 ban. 2 "Side": Chat (1); alice again (cert aa, other
// name), "Bob" with another cert, dave.
const data = `
INSERT INTO servers VALUES (1), (2);
INSERT INTO config VALUES (1, 'registername', 'Main'), (1, 'welcometext', 'Hello'), (2, 'registername', 'Side'), (2, 'defaultchannel', '1');
INSERT INTO channels VALUES (1, 0, NULL, 'Root', 1), (1, 1, 0, 'Lobby', 1), (1, 2, 1, 'Games', 0), (1, 3, 0, 'Music', 1), (2, 0, NULL, 'Root', 1), (2, 1, 0, 'Chat', 1);
INSERT INTO channel_info VALUES (1, 0, 0, 'Root text'), (1, 1, 1, '5'), (1, 2, 0, 'Play'), (1, 3, 2, '10');
INSERT INTO channel_links VALUES (1, 2, 3), (1, 3, 2);
INSERT INTO acl VALUES (1, 0, 1, NULL, 'admin', 1, 1, 0x1 | 0x10000, 0), (1, 2, 1, 2, NULL, 1, 0, 0x8, 0x4), (2, 0, 1, NULL, 'admin', 1, 1, 0x1, 0);
INSERT INTO groups VALUES (1, 1, 'admin', 0, 1, 1), (2, 2, 'admin', 0, 1, 1);
INSERT INTO group_members VALUES (1, 1, 1, 1), (1, 1, 2, 0), (2, 2, 3, 1);
INSERT INTO users (server_id, user_id, name, pw, lastchannel, texture) VALUES
  (1, 0, 'SuperUser', 'x', 0, NULL), (1, 1, 'alice', NULL, 2, NULL), (1, 2, 'bob', 'abc', 0, NULL), (1, 3, 'carol', 'abc', 0, NULL),
  (2, 0, 'SuperUser', 'x', 0, NULL), (2, 1, 'alice2', NULL, 1, NULL), (2, 2, 'Bob', NULL, 0, NULL), (2, 3, 'dave', NULL, 1, NULL);
INSERT INTO user_info VALUES (1, 1, 3, 'aa'), (1, 1, 1, 'alice@example.org'), (1, 1, 2, 'hi'), (1, 2, 3, 'bb'),
  (2, 1, 3, 'aa'), (2, 2, 3, 'b2'), (2, 3, 3, 'dd');
INSERT INTO bans VALUES (1, x'00000000000000000000ffff0a000000', 104, 'eve', 'ee', 'spam', '2026-01-02T03:04:05', 3600);
INSERT INTO channel_listeners (server_id, user_id, channel_id) VALUES (1, 1, 3);
`

func fixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(schema + data); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestServers(t *testing.T) {
	list, err := Servers(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0] != (ServerInfo{ID: 1, Name: "Main", Users: 3, Channels: 3}) || list[1].Name != "Side" {
		t.Fatalf("%+v", list)
	}
}

func TestRead(t *testing.T) {
	s, err := Read(fixture(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Main" || s.Welcome != "Hello" || s.Root.Description != "Root text" {
		t.Fatalf("server %+v", s)
	}
	if len(s.Root.ACLs) != 1 || s.Root.ACLs[0].Group != "admin" || s.Root.ACLs[0].Allow != mumbled.Write|mumbled.Kick {
		t.Errorf("root acl %+v", s.Root.ACLs)
	}
	g := s.Root.Groups["admin"]
	if g == nil || !g.Add[1] || !g.Remove[2] {
		t.Errorf("group %+v", g)
	}
	byID := map[uint32]mumbled.Channel{}
	for _, c := range s.Channels {
		byID[c.ID] = c
	}
	lobby, games, music := byID[1], byID[2], byID[3]
	if lobby.Name != "Lobby" || lobby.Position != 5 || games.Parent != 1 || games.Description != "Play" || !games.NoInheritACL || !games.Links[3] || !music.Links[2] {
		t.Errorf("channels %+v %+v %+v", lobby, games, music)
	}
	if a := games.ACLs; len(a) != 1 || !a[0].HasUser || a[0].UserID != 2 || a[0].Allow != mumbled.Speak || a[0].Deny != mumbled.Enter || a[0].ApplySubs {
		t.Errorf("games acl %+v", a)
	}
	if len(s.Users) != 2 || s.Users[0].Name != "alice" || s.Users[0].CertHash != "aa" || s.Users[0].Comment != "hi" || s.Users[0].LastChannel != 2 {
		t.Errorf("users %+v", s.Users)
	}
	if strings.Join(s.NoCert, ",") != "carol" {
		t.Errorf("no cert %v", s.NoCert)
	}
	if len(s.Bans) != 1 || s.Bans[0].Prefix != netip.MustParsePrefix("10.0.0.0/8") || s.Bans[0].Hash != "ee" || s.Bans[0].Start.Year() != 2026 || s.Bans[0].Duration.Hours() != 1 {
		t.Errorf("bans %+v", s.Bans)
	}
	if s.Skipped.Emails != 1 || s.Skipped.Passwords != 1 || s.Skipped.UserLimits != 1 || s.Skipped.Listeners != 1 {
		t.Errorf("skipped %+v", s.Skipped)
	}
	if _, err := Read(fixture(t), 7); err == nil {
		t.Error("an unknown server read")
	}
}

func TestPlanOneServer(t *testing.T) {
	s, _ := Read(fixture(t), 2)
	imp, rep := Plan([]Server{s}, true)
	if imp.Root == nil || imp.ServerName != "Side" || len(imp.Channels) != 1 || imp.Channels[0].ID != 1 || imp.Channels[0].Parent != 0 {
		t.Fatalf("import %+v", imp)
	}
	if len(imp.Users) != 3 || imp.Users[0].ID != 1 || len(rep.Renamed) != 0 {
		t.Fatalf("users %+v %+v", imp.Users, rep)
	}
	if !strings.Contains(rep.Notes(), "default channel") {
		t.Errorf("notes %q", rep.Notes())
	}
}

func TestPlanAllServers(t *testing.T) {
	db := fixture(t)
	main, _ := Read(db, 1)
	side, _ := Read(db, 2)
	imp, rep := Plan([]Server{main, side}, false)
	if imp.Root != nil || imp.ServerName != "" {
		t.Fatalf("root %+v", imp.Root)
	}
	byName := map[string]mumbled.Channel{}
	for _, c := range imp.Channels {
		byName[c.Name] = c
	}
	mainTop, sideTop := byName["Main"], byName["Side"]
	if mainTop.Parent != 0 || sideTop.Parent != 0 || byName["Lobby"].Parent != mainTop.ID || byName["Chat"].Parent != sideTop.ID || byName["Games"].Parent != byName["Lobby"].ID {
		t.Fatalf("tree %+v", byName)
	}
	if !byName["Games"].Links[byName["Music"].ID] || mainTop.Description != "Root text" || len(sideTop.ACLs) != 1 {
		t.Errorf("moved data %+v %+v", byName["Games"], mainTop)
	}
	users := map[string]mumbled.ImportUser{}
	for _, u := range imp.Users {
		users[u.Name] = u
	}
	// alice twice (one certificate): one user, both servers' groups.
	if len(imp.Users) != 4 || users["alice"].ID == 0 || users["alice2"].ID != 0 {
		t.Fatalf("users %+v", imp.Users)
	}
	if !sideTop.Groups["admin"].Add[users["dave"].ID] || !mainTop.Groups["admin"].Add[users["alice"].ID] {
		t.Errorf("groups %+v %+v", mainTop.Groups["admin"], sideTop.Groups["admin"])
	}
	// "Bob" with another certificate than bob: renamed.
	if _, ok := users["Bob-2"]; !ok || len(rep.Renamed) != 1 || rep.Renamed[0] != (Rename{Server: "Side", From: "Bob", To: "Bob-2"}) {
		t.Errorf("renamed %+v %+v", rep.Renamed, users)
	}
	if games := byName["Games"]; games.ACLs[0].UserID != users["bob"].ID {
		t.Errorf("acl user %+v", games.ACLs)
	}
	if users["alice"].LastChannel != byName["Games"].ID || users["dave"].LastChannel != byName["Chat"].ID {
		t.Errorf("last channels %+v", users)
	}
	if !strings.Contains(rep.Notes(), "Kick") {
		t.Errorf("notes %q", rep.Notes())
	}
}
