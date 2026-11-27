package site

import (
	"bytes"
	"compress/gzip"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A file that compresses about 3.5 to 1 and takes gzip a while, like a real
// wasm module: bytes drawn from a skewed spread, with some repeats.
func noisy(n int) []byte {
	b := make([]byte, n)
	rng := rand.New(rand.NewSource(1))
	for i := range b {
		if i > 64 && rng.Intn(4) == 0 {
			b[i] = b[i-1-rng.Intn(64)]
			continue
		}
		b[i] = byte(rng.Intn(1+rng.Intn(256)) * rng.Intn(3))
	}
	return b
}

func gzGet(h http.Handler, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Compressing a big file must not hold up the other assets, cached or not.
func TestCompressingOneFileDoesNotBlockOthers(t *testing.T) {
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

// Concurrent first requests for one file compress it once and all get it
// whole.
func TestConcurrentFirstRequestsCompressOnce(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	data := noisy(2 << 20)
	os.WriteFile(filepath.Join(dir, "assets", "a-1.wasm"), data, 0o644)
	h := New(http.NotFoundHandler(), dir, nil)

	var wg sync.WaitGroup
	got := make([][]byte, 8)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = gzGet(h, "/assets/a-1.wasm").Body.Bytes()
		}()
	}
	wg.Wait()
	for i, b := range got {
		if !bytes.Equal(b, got[0]) || !bytes.Equal(gunzip(t, b), data) {
			t.Fatalf("response %d differs", i)
		}
	}
}

// Counting compressions: the cache hands every caller the same slice.
func TestCacheCompressesOnce(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.js")
	os.WriteFile(file, noisy(1<<20), 0o644)
	c := &gzipCache{files: map[string]*gzipFile{}}
	var wg sync.WaitGroup
	got := make([][]byte, 8)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = c.get(file)
		}()
	}
	wg.Wait()
	for i := range got {
		if &got[i][0] != &got[0][0] {
			t.Fatalf("caller %d got its own copy: compressed more than once", i)
		}
	}
}

// A file that cannot be read is not cached: it is served once it exists.
func TestMissingFileIsRetried(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	h := New(http.NotFoundHandler(), dir, nil)
	if w := gzGet(h, "/assets/late-1.js"); w.Code != 404 {
		t.Fatalf("missing: %d", w.Code)
	}
	os.WriteFile(filepath.Join(dir, "assets", "late-1.js"), []byte("late"), 0o644)
	w := gzGet(h, "/assets/late-1.js")
	if w.Header().Get("Content-Encoding") != "gzip" || string(gunzip(t, w.Body.Bytes())) != "late" {
		t.Fatalf("after it appeared: %v", w.Header())
	}
}

func TestGzipHeaders(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "a-1.js"), bytes.Repeat([]byte("x"), 5000), 0o644)
	h := New(http.NotFoundHandler(), dir, nil)
	w := gzGet(h, "/assets/a-1.js")
	if w.Header().Get("Vary") != "Accept-Encoding" || w.Header().Get("Content-Encoding") != "gzip" ||
		w.Header().Get("Content-Length") == "" || w.Header().Get("Cache-Control") == "" {
		t.Fatalf("headers: %v", w.Header())
	}
	r := httptest.NewRequest("HEAD", "/assets/a-1.js", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	hw := httptest.NewRecorder()
	h.ServeHTTP(hw, r)
	if hw.Body.Len() != 0 || hw.Header().Get("Content-Length") != w.Header().Get("Content-Length") {
		t.Fatalf("head: %v, %d bytes", hw.Header(), hw.Body.Len())
	}
	pr := httptest.NewRecorder()
	h.ServeHTTP(pr, httptest.NewRequest("GET", "/assets/a-1.js", nil))
	if pr.Header().Get("Vary") != "Accept-Encoding" || pr.Header().Get("Content-Encoding") != "" || pr.Body.Len() != 5000 {
		t.Fatalf("plain: %v", pr.Header())
	}
}

// How long the first request for a 15 MB file holds the cache.
func BenchmarkFirstCompression15MB(b *testing.B) {
	file := filepath.Join(b.TempDir(), "big.wasm")
	os.WriteFile(file, noisy(15<<20), 0o644)
	b.ResetTimer()
	for b.Loop() {
		c := &gzipCache{files: map[string]*gzipFile{}}
		c.get(file)
	}
}
