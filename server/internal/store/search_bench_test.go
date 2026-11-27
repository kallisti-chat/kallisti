package store

import (
	"fmt"
	"testing"
)

// seedSearch fills the store with n messages in 20 channels from 200
// authors (user000 to user199), plus a few by "rare" in the last channel.
// It returns the channel ids.
func seedSearch(tb testing.TB, s *Store, n int) []int64 {
	tb.Helper()
	names := make([]string, 20)
	for i := range names {
		names[i] = fmt.Sprintf("channel%02d", i)
	}
	if err := s.EnsureChannels(names); err != nil {
		tb.Fatal(err)
	}
	cs, err := s.Channels()
	if err != nil {
		tb.Fatal(err)
	}
	var ids []int64
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	// Channel and author come from different divisors, so an author is
	// spread over all channels.
	_, err = s.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO messages (channel_id, author, text, created_at)
		SELECT c.id, printf('user%03d', i % 200), 'hello world ' || i, i
		FROM n JOIN (SELECT id, row_number() OVER (ORDER BY id) - 1 AS k FROM channels) c ON c.k = (i / 3) % 20`, n)
	if err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.db.Exec(`INSERT INTO messages (channel_id, author, text, created_at) VALUES (?, 'rare', 'hello rare', ?)`, ids[len(ids)-1], n+i); err != nil {
			tb.Fatal(err)
		}
	}
	return ids
}

// BenchmarkSearchByAuthor is the search by one author without words, in
// one channel and in all of them. user001 writes in the first channel and
// in some others; "rare" only in the last one, so in one channel it finds
// nothing, as the absent author does.
func BenchmarkSearchByAuthor(b *testing.B) {
	s, err := Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	ids := seedSearch(b, s, 120000)
	for _, where := range []struct {
		name     string
		channels []int64
	}{
		{"one channel", ids[:1]},
		{"all channels", ids},
	} {
		for _, who := range []struct{ name, author string }{
			{"common author", "user001"},
			{"rare author", "rare"},
			{"absent author", "nobody"},
		} {
			b.Run(where.name+"/"+who.name, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := s.Search("", where.channels, who.author, 50); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
