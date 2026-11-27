package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func hasIndex(t testing.TB, s *Store, name string) bool {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestSessionsAreIndexedByAccount(t *testing.T) {
	if !hasIndex(t, open(t), "sessions_by_account") {
		t.Fatal("a new database has no sessions_by_account index")
	}
}

func TestSessionIndexIsAddedToAnOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	acc, err := s.CreateAccount(Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("old-session", acc.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Simulate a database from before the index.
	if _, err := s.db.Exec(`DROP INDEX sessions_by_account`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	for i := 0; i < 2; i++ { // the second open: nothing to add, and no error
		s, err = Open(path)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		if !hasIndex(t, s, "sessions_by_account") {
			t.Fatalf("reopen %d: the index was not added", i)
		}
		if got, err := s.AccountBySession("old-session", now); err != nil || got.ID != acc.ID {
			t.Fatalf("reopen %d: the session is lost: %+v %v", i, got, err)
		}
		s.Close()
	}
}

// BenchmarkPushSubscriptions is the push lookup of one account among many
// accounts with sessions that all have a subscription.
func BenchmarkPushSubscriptions(b *testing.B) {
	for _, indexed := range []bool{false, true} {
		b.Run(fmt.Sprintf("indexed=%v", indexed), func(b *testing.B) {
			s, err := Open(":memory:")
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			if !indexed {
				if _, err := s.db.Exec(`DROP INDEX sessions_by_account`); err != nil {
					b.Fatal(err)
				}
			}
			now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
			const accounts, sessionsEach = 2000, 5
			for i := 0; i < accounts; i++ {
				acc, err := s.CreateAccount(Account{Name: fmt.Sprintf("user%d", i), PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
				if err != nil {
					b.Fatal(err)
				}
				for j := 0; j < sessionsEach; j++ {
					hash := fmt.Sprintf("s%d-%d", i, j)
					if err := s.CreateSession(hash, acc.ID, now.Add(time.Hour)); err != nil {
						b.Fatal(err)
					}
					sub := PushSubscription{Endpoint: "https://push.example/" + hash, P256dh: "p", Auth: "a"}
					if err := s.SetPushSubscription(hash, sub, now); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				subs, err := s.PushSubscriptions("user1234", now)
				if err != nil || len(subs) != sessionsEach {
					b.Fatalf("%d subscriptions, %v", len(subs), err)
				}
			}
		})
	}
}
