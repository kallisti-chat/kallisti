package store

import "testing"

// seedMessages fills one channel with n plain messages, ids 1 to n.
func seedMessages(tb testing.TB, s *Store, n int) int64 {
	tb.Helper()
	if err := s.EnsureChannels([]string{"general"}); err != nil {
		tb.Fatal(err)
	}
	cs, err := s.Channels()
	if err != nil {
		tb.Fatal(err)
	}
	_, err = s.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO messages (channel_id, author, text, created_at) SELECT ?, 'alice', 'hello', i FROM n`, n, cs[0].ID)
	if err != nil {
		tb.Fatal(err)
	}
	return cs[0].ID
}

func BenchmarkHistoryDeepScrollback(b *testing.B) {
	s, err := Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	ch := seedMessages(b, s, 100000)
	for _, c := range []struct {
		name   string
		before int64
	}{
		{"newest", 0},
		{"deep", 100},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := s.History(ch, c.before, 50); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
