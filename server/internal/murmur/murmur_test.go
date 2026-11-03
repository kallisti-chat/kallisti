package murmur

import (
	"database/sql"
	"net/netip"
	"os"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"kallisti.chat/server/internal/mumbled"
)

func fixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	script, err := os.ReadFile("testdata/murmur.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
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
