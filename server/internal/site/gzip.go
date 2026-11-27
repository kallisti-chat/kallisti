package site

import (
	"bytes"
	"compress/gzip"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Assets that compress well (the DeepFilterNet3 engine: 14.6 MB, 4 MB with
// gzip). Assets never change (a hash in the name): each is compressed once
// and kept.
var compressible = []string{".js", ".css", ".wasm", ".svg", ".json"}

type gzipCache struct {
	mu    sync.Mutex // only for the map: never held while compressing
	files map[string]*gzipFile
}

// One file's compression: the first request does it, the others for the
// same file wait for it, and no other file waits.
type gzipFile struct {
	once sync.Once
	body []byte
	ok   bool
}

// serve answers with the gzipped asset if the client takes gzip and the
// file compresses (false: serve it as it is).
func (c *gzipCache) serve(w http.ResponseWriter, r *http.Request, dir, clean string) bool {
	ext := filepath.Ext(clean)
	if !slices.Contains(compressible, ext) {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding") // also on the plain answer, for caches
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		return false
	}
	body, ok := c.get(filepath.Join(dir, filepath.FromSlash(clean)))
	if !ok {
		return false
	}
	h := w.Header()
	h.Set("Content-Type", mime.TypeByExtension(ext))
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		w.Write(body)
	}
	return true
}

func (c *gzipCache) get(file string) ([]byte, bool) {
	c.mu.Lock()
	f := c.files[file]
	if f == nil {
		f = &gzipFile{}
		c.files[file] = f
	}
	c.mu.Unlock()
	f.once.Do(func() {
		data, err := os.ReadFile(file) // #nosec G304 G703 -- under webDir: a rooted path.Clean without a backslash (site.go)
		if err != nil {
			return
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf) // the best level gains 0.2% and takes longer
		zw.Write(data)
		zw.Close()
		f.body, f.ok = buf.Bytes(), true
	})
	if !f.ok {
		// Not cached: the next request tries again.
		c.mu.Lock()
		if c.files[file] == f {
			delete(c.files, file)
		}
		c.mu.Unlock()
	}
	return f.body, f.ok
}
