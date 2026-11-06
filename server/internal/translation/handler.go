package translation

import (
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
	sum, ok := strings.CutPrefix(r.URL.Path, "/api/translation/files/")
	pair, f, found := h.find(sum)
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(h.Dir, f.SHA256)
	if _, err := os.Stat(path); err != nil {
		if err := h.fetchOnce(pair, f); err != nil {
			slog.Error("translation model", "pair", pair, "file", f.Name, "err", err)
			http.Error(w, "cannot get the model", http.StatusBadGateway)
			return
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(map[string]any{"pairs": pairs})
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

// fetchOnce fetches a file, or waits for the fetch already running.
func (h *Handler) fetchOnce(pair string, f File) error {
	h.mu.Lock()
	l := h.loading[f.SHA256]
	if l == nil {
		l = &load{done: make(chan struct{})}
		if h.loading == nil {
			h.loading = map[string]*load{}
		}
		h.loading[f.SHA256] = l
		go func() {
			l.err = h.fetch(pair, f)
			h.mu.Lock()
			delete(h.loading, f.SHA256)
			h.mu.Unlock()
			close(l.done)
		}()
	}
	h.mu.Unlock()
	<-l.done
	return l.err
}

func (h *Handler) fetch(pair string, f File) error {
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	res, err := client.Get(h.Manifest.Source + pair + "/" + f.Name)
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
