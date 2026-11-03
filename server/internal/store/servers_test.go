package store

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestAccountServers(t *testing.T) {
	s := open(t)
	now := time.Now()
	acc, _ := s.CreateAccount(Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	other, _ := s.CreateAccount(Account{Name: "bob", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	origins := func(id int64) []string {
		list, err := s.AccountServers(id)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, x := range list {
			out = append(out, x.Origin+" "+x.Name)
		}
		return out
	}
	for _, x := range []AccountServer{{"https://b.example", "B"}, {"https://a.example", "A"}} {
		if err := s.AddAccountServer(acc.ID, x, 2); err != nil {
			t.Fatal(err)
		}
	}
	// In the order added; adding again renames, keeps its place.
	if err := s.AddAccountServer(acc.ID, AccountServer{"https://b.example", "Bee"}, 2); err != nil {
		t.Fatal(err)
	}
	if got := origins(acc.ID); !slices.Equal(got, []string{"https://b.example Bee", "https://a.example A"}) {
		t.Fatalf("list %v", got)
	}
	if err := s.AddAccountServer(acc.ID, AccountServer{"https://c.example", "C"}, 2); !errors.Is(err, ErrTooManyServers) {
		t.Fatalf("over the limit: %v", err)
	}
	if got := origins(other.ID); len(got) != 0 {
		t.Fatalf("another account's: %v", got)
	}
	if err := s.RemoveAccountServer(acc.ID, "https://b.example"); err != nil {
		t.Fatal(err)
	}
	if got := origins(acc.ID); !slices.Equal(got, []string{"https://a.example A"}) {
		t.Fatalf("after remove %v", got)
	}
	// Gone with the account.
	if err := s.DeleteAccount(acc.ID); err != nil {
		t.Fatal(err)
	}
	if got := origins(acc.ID); len(got) != 0 {
		t.Fatalf("after delete %v", got)
	}
}
