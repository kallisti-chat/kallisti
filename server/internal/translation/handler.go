package translation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kallisti.chat/server/internal/config"
)

// Handler serves GET /api/translation/models (the pairs, their files by
// hash) and /api/translation/files/<sha256>: from Dir, else fetched from
// the manifest's source first (once, also for many asking at the same
// time). Files are public: no session (the apps' requests have none).
type Handler struct {
	Manifest Manifest
	Dir      string
	Client   *http.Client // nil: one with a time limit

	mu      sync.Mutex
	loading map[string]*load
	clean   sync.Once // temporary files of a stopped server
}

type load struct {
	done chan struct{}
	err  error
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	config.CORS(w, r.Header.Get("Origin"), "GET", "", false)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/api/translation/models" {
		h.list(w)
		return
	}
	if path, ok := strings.CutPrefix(r.URL.Path, "/api/translation/llm/"); ok {
		f, found := h.Manifest.LLM[path]
		if !found {
			http.NotFound(w, r)
			return
		}
		h.serve(w, r, h.Manifest.LLMSource+path, f)
		return
	}
	sum, ok := strings.CutPrefix(r.URL.Path, "/api/translation/files/")
	pair, f, found := h.find(sum)
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	h.serve(w, r, h.Manifest.Source+pair+"/"+f.Name, f)
}

// serve answers with a file, fetched from url first if it is not kept.
func (h *Handler) serve(w http.ResponseWriter, r *http.Request, url string, f File) {
	path := filepath.Join(h.Dir, f.SHA256)
	if _, err := os.Stat(path); err != nil {
		if err := h.fetchOnce(r.Context(), url, f); err != nil {
			slog.Error("translation model", "url", url, "err", err)
			http.Error(w, "cannot get the model", http.StatusBadGateway)
			return
		}
	}
	contentType := "application/octet-stream"
	if strings.HasSuffix(f.Name, ".json") {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

func (h *Handler) list(w http.ResponseWriter) {
	type part struct {
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}
	pairs := map[string]map[string]part{}
	for pair, files := range h.Manifest.Pairs {
		pairs[pair] = map[string]part{}
		for p, f := range files {
			pairs[pair][p] = part{f.SHA256, f.Size}
		}
	}
	out := map[string]any{"pairs": pairs}
	if len(h.Manifest.LLM) > 0 {
		var size int64
		for _, f := range h.Manifest.LLM {
			size += f.Size
		}
		out["llm"] = map[string]int64{"size": size}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handler) find(sum string) (string, File, bool) {
	for pair, files := range h.Manifest.Pairs {
		for _, f := range files {
			if f.SHA256 == sum {
				return pair, f, true
			}
		}
	}
	return "", File{}, false
}

// fetchOnce fetches a file, or waits for the fetch already running (until
// the client goes).
func (h *Handler) fetchOnce(ctx context.Context, url string, f File) error {
	h.clean.Do(func() {
		old, _ := filepath.Glob(filepath.Join(h.Dir, ".fetch-*"))
		for _, p := range old {
			os.Remove(p)
		}
	})
	h.mu.Lock()
	if _, err := os.Stat(filepath.Join(h.Dir, f.SHA256)); err == nil { // kept meanwhile
		h.mu.Unlock()
		return nil
	}
	l := h.loading[f.SHA256]
	if l == nil {
		l = &load{done: make(chan struct{})}
		if h.loading == nil {
			h.loading = map[string]*load{}
		}
		h.loading[f.SHA256] = l
		go func() {
			l.err = h.fetch(url, f)
			h.mu.Lock()
			delete(h.loading, f.SHA256)
			h.mu.Unlock()
			close(l.done)
		}()
	}
	h.mu.Unlock()
	select {
	case <-l.done:
		return l.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Handler) fetch(url string, f File) error {
	client := h.Client
	if client == nil {
		// Ten minutes and 256 KiB/s (the larger model has 1.4 GB).
		client = &http.Client{Timeout: 10*time.Minute + time.Duration(f.Size>>18)*time.Second}
	}
	res, err := client.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}
	if err := os.MkdirAll(h.Dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(h.Dir, ".fetch-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(res.Body, f.Size+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(sum.Sum(nil)); n != f.Size || got != f.SHA256 {
		return fmt.Errorf("got %d bytes with SHA-256 %s", n, got)
	}
	return os.Rename(tmp.Name(), filepath.Join(h.Dir, f.SHA256))
}
