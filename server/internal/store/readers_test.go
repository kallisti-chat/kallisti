package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// In a file the store reads on connections of its own. A read must still
// see every write that returned before it: the web client loads a message
// right after it sent it.
func TestReadsSeeTheWriteBeforeThem(t *testing.T) {
	s := fileStore(t)
	if s.rdb == s.db {
		t.Fatal("a store in a file has no connections for reading")
	}
	ch := seedMessages(t, s, 10)
	at := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for i := range 300 {
		text := fmt.Sprint("needle", i)
		m, err := s.AddMessage(NewMessage{ChannelID: ch, Author: "alice", Text: text, At: at})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := s.MessageByID(m.ID); err != nil || got.Text != text {
			t.Fatalf("by id after send %d: %q %v", i, got.Text, err)
		}
		page, err := s.History(ch, 0, 1)
		if err != nil || len(page) != 1 || page[0].ID != m.ID {
			t.Fatalf("history after send %d: %v %v", i, page, err)
		}
		found, err := s.Search(text, []int64{ch}, "", 5)
		if err != nil || len(found) != 1 || found[0].ID != m.ID {
			t.Fatalf("search after send %d: %v %v", i, found, err)
		}
		if _, err := s.DeleteMessage(m.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.MessageByID(m.ID); err != ErrNotFound {
			t.Fatalf("by id after delete %d: %v", i, err)
		}
	}
}

// Sends and reads at the same time, for the race detector, and each
// sender finds its own message at once.
func TestReadsAndWritesAtTheSameTime(t *testing.T) {
	s := fileStore(t)
	ch := seedMessages(t, s, 1000)
	at := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			for i := range 50 {
				m, err := s.AddMessage(NewMessage{ChannelID: ch, Author: fmt.Sprint("user", w), Text: "hello", At: at})
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := s.MessageByID(m.ID); err != nil {
					t.Errorf("sender %d, message %d: %v", w, i, err)
					return
				}
			}
		})
		wg.Go(func() {
			for range 50 {
				if page, err := s.History(ch, 0, 50); err != nil || len(page) != 50 {
					t.Errorf("history: %d %v", len(page), err)
					return
				}
				if _, err := s.Search("hello", []int64{ch}, "", 20); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestTheReadingConnectionsCannotWrite(t *testing.T) {
	s := fileStore(t)
	if _, err := s.rdb.Exec(`DELETE FROM channels`); err == nil {
		t.Fatal("a write on a reading connection went through")
	}
}

// In memory a second connection would be another, empty database.
func TestAStoreInMemoryHasOneConnection(t *testing.T) {
	if s := open(t); s.rdb != s.db {
		t.Fatal("a store in memory reads on other connections")
	}
}
