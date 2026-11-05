package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/auth"
	"kallisti.chat/server/internal/certs"
	"kallisti.chat/server/internal/files"
	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/sidecar"
	"kallisti.chat/server/internal/store"
)

// Exercise real sessions, certificate identities, room ACLs, upload/message
// lookup and HTTP responses together. No chat connection is required.
func TestPrivateRoomMediaSessions(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mdb, err := mumbled.OpenDB(db.DB())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	accounts := &auth.Service{Store: db, Now: func() time.Time { return now }, AllowedOrigins: []string{"https://chat.example"}, TrustForwardedFor: true}
	hashToken := func(token string) string { sum := sha256.Sum256([]byte(token)); return hex.EncodeToString(sum[:]) }
	makeAccount := func(name, token string) store.Account {
		t.Helper()
		cert, key, err := certs.Generate(name)
		if err != nil {
			t.Fatal(err)
		}
		a, err := db.CreateAccount(store.Account{Name: name, CertPEM: cert, KeyPEM: key}, now, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.CreateSession(hashToken(token), a.ID, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		return a
	}
	alice := makeAccount("alice", "alice-session")
	makeAccount("bob", "bob-session")
	// A mismatched display name deliberately verifies that identity comes
	// from the certificate, not from looking up the account's name.
	user, err := mdb.RegisterUser("bob", accountCertHash(alice))
	if err != nil {
		t.Fatal(err)
	}
	const room = uint32(100)
	if err := mdb.SaveChannel(&mumbled.Channel{ID: room, Name: "food-pictures", ACLs: []mumbled.ACL{
		{ApplyHere: true, ApplySubs: true, Group: "all", Deny: mumbled.Traverse | mumbled.Enter},
		{ApplyHere: true, ApplySubs: true, HasUser: true, UserID: user, Allow: mumbled.Traverse | mumbled.Enter},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := mdb.SaveNextChannel(room); err != nil {
		t.Fatal(err)
	}
	voice, err := mumbled.New(mumbled.Config{DB: mdb})
	if err != nil {
		t.Fatal(err)
	}
	defer voice.Close()
	hub, err := sidecar.NewHub(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	hub.SetVoice(voiceServer{Server: voice}, "")
	chat, err := db.VoiceChat(room)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 48)
	content := []byte("private food picture fixture")
	u, err := db.AddUpload(store.Upload{Key: key, Name: "food.txt", Type: "text/plain", Size: int64(len(content)), Author: "alice"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddMessage(store.NewMessage{ChannelID: chat, Author: "alice", Text: "food", At: now, Uploads: []store.Attachment{{ID: u.ID}}}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, key), content, 0600); err != nil {
		t.Fatal(err)
	}
	handler := &files.Handler{Store: db, Dir: dir, AuthorizeRead: privateRoomMedia(accounts, hub, voice)}
	for _, c := range []struct {
		name, token, origin string
		bearer, query       bool
		want                int
	}{
		{name: "anonymous", want: 404},
		{name: "member cookie", token: "alice-session", want: 200},
		{name: "member same-origin", token: "alice-session", origin: "https://chat.example", want: 200},
		{name: "member bearer", token: "alice-session", bearer: true, want: 200},
		{name: "outsider with member display name", token: "bob-session", want: 404},
		{name: "invalid session", token: "invalid", want: 404},
		{name: "cross-origin cookie", token: "alice-session", origin: "https://other.example", want: 404},
		{name: "token in URL grants nothing", token: "alice-session", query: true, want: 404},
	} {
		t.Run(c.name, func(t *testing.T) {
			url := sidecar.FileURL(u)
			if c.query {
				url += "?token=" + c.token
			}
			r := httptest.NewRequest("GET", url, nil)
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("Origin", c.origin)
			if !c.query && c.token != "" {
				if c.bearer {
					r.Header.Set("Authorization", "Bearer "+c.token)
				} else {
					r.AddCookie(&http.Cookie{Name: "__Host-" + auth.CookieName, Value: c.token})
				}
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != c.want || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("got %d %v", w.Code, w.Header())
			}
			if c.want == 200 && w.Body.String() != string(content) {
				t.Fatal("wrong media body")
			}
		})
	}
	checkDenied := func() {
		t.Helper()
		r := httptest.NewRequest("GET", sidecar.FileURL(u), nil)
		r.Header.Set("Authorization", "Bearer alice-session")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("session still readable: %d", w.Code)
		}
	}
	now = now.Add(2 * time.Hour)
	checkDenied() // expired
	now = now.Add(-2 * time.Hour)
	if err := db.DeleteSession(hashToken("alice-session")); err != nil {
		t.Fatal(err)
	}
	checkDenied() // logged out
	if err := db.CreateSession(hashToken("alice-session"), alice.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := mdb.UnregisterUser(user); err != nil {
		t.Fatal(err)
	}
	checkDenied() // Mumble registration removed, even though web session exists
}
