//go:build !race

package site

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Compressing a big file must not hold up the other assets, cached or not.
// It measures time, and the race detector makes the large compression take
// many seconds, so it is not built under -race.
func TestCompressingOneFileDoesNotBlockOthers(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	bigData := noisy(16 << 20)
	small := bytes.Repeat([]byte("small "), 1000)
	os.WriteFile(filepath.Join(dir, "assets", "big-1.wasm"), bigData, 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "cached-1.js"), small, 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "fresh-1.js"), small, 0o644)
	h := New(http.NotFoundHandler(), dir, nil)
	gzGet(h, "/assets/cached-1.js") // now cached

	bigTook := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		gzGet(h, "/assets/big-1.wasm")
		bigTook <- time.Since(start)
	}()
	time.Sleep(10 * time.Millisecond) // the big file is being compressed by now

	// Blocked behind the big file, a request takes nearly as long as it
	// does; free, it takes microseconds. Compare with half as the margin.
	var took []time.Duration
	for _, p := range []string{"/assets/cached-1.js", "/assets/fresh-1.js"} {
		start := time.Now()
		w := gzGet(h, p)
		took = append(took, time.Since(start))
		if !bytes.Equal(gunzip(t, w.Body.Bytes()), small) {
			t.Fatalf("%s: wrong content", p)
		}
	}
	big := <-bigTook
	if big < 100*time.Millisecond {
		t.Skipf("the big file compressed in %v: too fast to tell", big)
	}
	for i, d := range took {
		if d > big/2 {
			t.Errorf("request %d took %v while the big file took %v: it waited for it", i, d, big)
		}
	}
}
