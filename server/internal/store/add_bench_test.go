package store

import (
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkAddMessage is storing a plain message (no files, no quote) in
// a database in a file, as the server has it.
func BenchmarkAddMessage(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "k.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	ch := seedMessages(b, s, 1000)
	at := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.AddMessage(NewMessage{ChannelID: ch, Author: "alice", Text: "hello", At: at}); err != nil {
			b.Fatal(err)
		}
	}
}
