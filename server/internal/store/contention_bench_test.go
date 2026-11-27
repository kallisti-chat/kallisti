package store

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fileStore is a store in a file, as the server has it: an in-memory one
// has a single connection.
func fileStore(tb testing.TB) *Store {
	tb.Helper()
	s, err := Open(filepath.Join(tb.TempDir(), "k.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { s.Close() })
	return s
}

// BenchmarkHistoryInParallel is many clients loading the newest page at
// once.
func BenchmarkHistoryInParallel(b *testing.B) {
	s := fileStore(b)
	ch := seedMessages(b, s, 100000)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := s.History(ch, 0, 50); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkSendDuringSearches is sending a message while two clients
// search a long channel for an author who wrote nothing there (the slow
// case: it reads the whole channel).
func BenchmarkSendDuringSearches(b *testing.B) {
	s := fileStore(b)
	ch := seedMessages(b, s, 100000)
	var stop atomic.Bool
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for !stop.Load() {
				if _, err := s.Search("", []int64{ch}, "nobody", 50); err != nil {
					b.Error(err)
					return
				}
			}
		})
	}
	at := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.AddMessage(NewMessage{ChannelID: ch, Author: "alice", Text: "hello", At: at}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	stop.Store(true)
	wg.Wait()
}
