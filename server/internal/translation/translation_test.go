package translation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestParts(t *testing.T) {
	for name, want := range map[string]string{
		"model.zhen.intgemm.alphas.bin": "model", "lex.50.50.zhen.s2t.bin": "lex", "vocab.zhen.spm": "vocab",
		"srcvocab.enzh.spm": "srcvocab", "trgvocab.enzh.spm": "trgvocab", "README.md": "",
	} {
		if got := Part(name); got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// The bundled list has the pairs of our languages, both ways; a server
// offers only the languages it is set to.
func TestBundledAndLanguages(t *testing.T) {
	m := Bundled()
	for _, pair := range []string{"zh-en", "en-zh", "zh-Hant-en", "en-zh-Hant", "de-en", "en-ja"} {
		if len(m.Pairs[pair]) < 3 {
			t.Fatalf("%s: %v", pair, m.Pairs[pair])
		}
	}
	got := m.For([]string{"zh", "de"})
	if len(got.Pairs) != 4 || got.Pairs["zh-en"] == nil || got.Pairs["en-de"] == nil || got.Pairs["zh-Hant-en"] != nil {
		t.Fatalf("%v", got.Pairs)
	}
	if len(m.For([]string{"*"}).Pairs) != len(m.Pairs) {
		t.Fatal("* is not all")
	}
}

func hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestFilesAreFetchedOnceCheckedAndKept(t *testing.T) {
	model, vocab := []byte("model bytes"), []byte("vocab bytes")
	var hits atomic.Int32
	bad := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/rev/zh-en/model.zhen.bin":
			if bad {
				w.Write([]byte("other bytes"))
				return
			}
			w.Write(model)
		case "/rev/zh-en/vocab.zhen.spm":
			w.Write(vocab)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	m := Manifest{Source: upstream.URL + "/rev/", Pairs: map[string]map[string]File{"zh-en": {
		"model": {Name: "model.zhen.bin", Size: int64(len(model)), SHA256: hash(model)},
		"vocab": {Name: "vocab.zhen.spm", Size: int64(len(vocab)), SHA256: hash(vocab)},
	}}}
	dir := t.TempDir()
	h := &Handler{Manifest: m, Dir: dir}
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Origin", "tauri://localhost")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// The list: parts by hash, no upstream names.
	w := get("/api/translation/models")
	var list struct {
		Pairs map[string]map[string]struct {
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Pairs["zh-en"]["model"].SHA256 != hash(model) || w.Header().Get("Access-Control-Allow-Origin") != "tauri://localhost" {
		t.Fatalf("list: %s %v", w.Body, w.Header())
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if w := get("/api/translation/files/" + hash(model)); w.Code != 200 || w.Body.String() != string(model) {
				t.Errorf("model: %d %q", w.Code, w.Body)
			}
		})
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Fatalf("fetched %d times", hits.Load())
	}
	if w := get("/api/translation/files/" + hash(model)); w.Code != 200 || hits.Load() != 1 || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("again: %d, %d fetches, %v", w.Code, hits.Load(), w.Header())
	}
	if w := get("/api/translation/files/" + hash([]byte("unknown"))); w.Code != 404 {
		t.Fatalf("unknown: %d", w.Code)
	}
	// A wrong file from upstream is not kept or served.
	os.Remove(filepath.Join(dir, hash(model)))
	bad = true
	if w := get("/api/translation/files/" + hash(model)); w.Code != http.StatusBadGateway {
		t.Fatalf("wrong bytes: %d", w.Code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("kept: %v", entries)
	}
}
