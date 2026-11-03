package store

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestFriends(t *testing.T) {
	s := open(t)
	acc, _ := s.CreateAccount(Account{Name: "alice", CertPEM: []byte("c"), KeyPEM: []byte("k")}, time.Now(), "")
	if list, err := s.Friends(acc.ID); err != nil || len(list) != 0 {
		t.Fatalf("none yet: %v %v", list, err)
	}
	bob := Friend{Origin: "https://b.example", User: 7, Name: "bob"}
	carol := Friend{Origin: "https://a.example", User: 3, Name: "carol"}
	s.AddFriend(acc.ID, bob, 10)
	s.AddFriend(acc.ID, carol, 10)
	// Again: the new name, the same place.
	s.AddFriend(acc.ID, Friend{Origin: "https://b.example", User: 7, Name: "bobby"}, 10)
	if list, _ := s.Friends(acc.ID); !slices.Equal(list, []Friend{{Origin: "https://b.example", User: 7, Name: "bobby"}, carol}) {
		t.Fatalf("list %v", list)
	}
	if err := s.AddFriend(acc.ID, Friend{Origin: "https://c.example", User: 1, Name: "x"}, 2); !errors.Is(err, ErrTooManyFriends) {
		t.Fatalf("over the limit: %v", err)
	}
	s.RemoveFriend(acc.ID, "https://b.example", 7)
	if list, _ := s.Friends(acc.ID); !slices.Equal(list, []Friend{carol}) {
		t.Fatalf("after remove %v", list)
	}
}
