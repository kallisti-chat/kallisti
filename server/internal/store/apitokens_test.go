package store

import (
	"errors"
	"testing"
	"time"
)

func TestAPITokens(t *testing.T) {
	s := open(t)
	now := time.UnixMilli(1_700_000_000_000)
	acc, _ := s.CreateAccount(Account{Name: "alice", PasswordHash: "x", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	bob, _ := s.CreateAccount(Account{Name: "bob", PasswordHash: "x", CertPEM: []byte("c2"), KeyPEM: []byte("k2")}, now, "")

	tok, err := s.CreateAPIToken(acc.ID, "my agent", "hash1", true, now)
	if err != nil || tok.Name != "my agent" || !tok.Write {
		t.Fatalf("create: %+v %v", tok, err)
	}
	s.CreateAPIToken(acc.ID, "reader", "hash2", false, now)
	if got, w, err := s.AccountByAPIToken("hash1"); err != nil || got.ID != acc.ID || !w {
		t.Fatalf("lookup: %v %v %v", got.Name, w, err)
	}
	if list, _ := s.APITokens(acc.ID); len(list) != 2 || list[0].Name != "my agent" {
		t.Fatalf("list: %+v", list)
	}
	// Only the owner removes a token.
	if err := s.DeleteAPIToken(bob.ID, tok.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's token: %v", err)
	}
	if err := s.DeleteAPIToken(acc.ID, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AccountByAPIToken("hash1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token: %v", err)
	}
	// All of an account's tokens end (see auth's endLogins).
	if err := s.DeleteAPITokens(acc.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.APITokens(acc.ID); len(list) != 0 {
		t.Fatalf("after ending all: %+v", list)
	}
	// At most maxAPITokens per account.
	for i := range maxAPITokens {
		if _, err := s.CreateAPIToken(bob.ID, "t", "b"+string(rune('a'+i)), false, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateAPIToken(bob.ID, "one more", "bz", false, now); !errors.Is(err, ErrTooManyTokens) {
		t.Fatalf("over the limit: %v", err)
	}
}

// Messages posted with an access token keep their bot mark.
func TestBotMessages(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	cs, _ := s.Channels()
	now := time.UnixMilli(1_700_000_000_000)
	m, err := s.AddMessage(NewMessage{ChannelID: cs[0].ID, Author: "alice", Text: "beep", At: now, Bot: true})
	if err != nil || !m.Bot {
		t.Fatalf("add: %+v %v", m.Bot, err)
	}
	h, _ := s.History(cs[0].ID, 0, 10)
	if len(h) != 1 || !h[0].Bot {
		t.Fatalf("history: %+v", h)
	}
}
