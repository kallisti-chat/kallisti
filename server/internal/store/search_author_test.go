package store

import (
	"path/filepath"
	"testing"
)

// oldSearch is Search as it was before the index on the author: one query
// with an OR for the author.
func oldSearch(s *Store, text string, channels []int64, author string, limit int) ([]Message, error) {
	if len(channels) == 0 {
		return nil, nil
	}
	where, args := `1`, []any{}
	if q := ftsQuery(text); q != "" {
		where, args = `m.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)`, append(args, q)
	}
	in, chans := inList(channels)
	args = append(append(args, chans...), author, author, limit)
	rows, err := s.db.Query(messageSelect+`WHERE `+where+`
		AND m.channel_id IN (`+in+`)
		AND (? = '' OR lower(m.author) = lower(?)) ORDER BY m.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func TestSearchByAuthorGivesWhatItDid(t *testing.T) {
	s := open(t)
	ids := seedSearch(t, s, 3000)
	// Authors in other cases, and one that lower() leaves alone.
	for _, q := range []string{
		`UPDATE messages SET author = 'USER007' WHERE author = 'user007' AND id % 2 = 0`,
		`UPDATE messages SET author = 'User007' WHERE author = 'user007' AND id % 3 = 0`,
		`UPDATE messages SET author = 'Ärger' WHERE author = 'user010'`,
		`UPDATE messages SET text = 'goodbye world' WHERE id % 5 = 0`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name     string
		text     string
		channels []int64
		author   string
		limit    int
		min      int // at least this many messages, so the case tests something
	}{
		{"author only", "", ids, "user007", 50, 15},
		{"mixed case author", "", ids, "UsEr007", 50, 15},
		{"mixed case in the data", "", ids, "user007", 1000, 15},
		{"author with no messages", "", ids, "nobody", 50, 0},
		{"words only", "hello", ids, "", 50, 50},
		{"words and author", "hello wor", ids, "USER007", 50, 10},
		{"words and author, other words", "goodbye", ids, "user005", 50, 1},
		{"one channel", "", ids[:1], "user001", 50, 3},
		{"several channels", "", ids[2:7], "user008", 50, 1},
		{"author outside the channels", "", ids[:5], "rare", 50, 0},
		{"author in the last channel", "", ids[len(ids)-1:], "rare", 50, 3},
		{"limit under the matches", "", ids, "user011", 3, 3},
		{"limit one", "", ids, "user011", 1, 1},
		{"limit over the matches", "", ids, "rare", 1000, 3},
		{"non-ASCII author", "", ids, "Ärger", 50, 15},
		{"non-ASCII author, other case", "", ids, "ärger", 50, 0},
		{"no channels", "", nil, "user007", 50, 0},
		{"no channels, no author", "hello", []int64{}, "", 50, 0},
		{"punctuation only", "***", ids, "user007", 50, 15},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want, err := oldSearch(s, c.text, c.channels, c.author, c.limit)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Search(c.text, c.channels, c.author, c.limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) < c.min {
				t.Fatalf("%d messages, want at least %d", len(got), c.min)
			}
			if len(got) != len(want) {
				t.Fatalf("%d messages, was %d", len(got), len(want))
			}
			for i := range got { // the same messages in the same order
				if got[i].ID != want[i].ID || got[i].Author != want[i].Author {
					t.Fatalf("message %d: %d by %q, was %d by %q", i, got[i].ID, got[i].Author, want[i].ID, want[i].Author)
				}
			}
		})
	}
}

func TestMessagesAreIndexedByAuthor(t *testing.T) {
	if !hasIndex(t, open(t), "messages_by_author") {
		t.Fatal("a new database has no messages_by_author index")
	}
}

func TestAuthorIndexIsAddedToAnOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ids := seedSearch(t, s, 500)
	// Simulate a database from before the index.
	if _, err := s.db.Exec(`DROP INDEX messages_by_author`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	for i := 0; i < 2; i++ { // the second open: nothing to add, and no error
		s, err = Open(path)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		if !hasIndex(t, s, "messages_by_author") {
			t.Fatalf("reopen %d: the index was not added", i)
		}
		got, err := s.Search("", ids, "USER007", 1000)
		if err != nil || len(got) != 3 { // 500 messages, every 200th: 7, 207, 407
			t.Fatalf("reopen %d: %d messages, %v", i, len(got), err)
		}
		s.Close()
	}
}
