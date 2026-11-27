package site

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func webDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644)
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "app-123.js"), []byte("console.log(1)"), 0o644)
	return dir
}

var bridge = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "bridge") })

func get(t *testing.T, h http.Handler, path string, upgrade bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	if upgrade {
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestWebSocketsGoToTheBridge(t *testing.T) {
	h := New(bridge, webDir(t), nil)
	for _, path := range []string{"/ws", "/"} {
		if body := get(t, h, path, true).Body.String(); body != "bridge" {
			t.Errorf("%s upgrade: got %q", path, body)
		}
	}
}

func TestServesTheWebApp(t *testing.T) {
	h := New(bridge, webDir(t), nil)
	w := get(t, h, "/", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "app") {
		t.Fatalf("index: %d %q", w.Code, w.Body.String())
	}
	// data: images: avatars (Mumble textures) are data: URLs.
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") ||
		!strings.Contains(csp, "img-src 'self' blob: data: https:;") || !strings.Contains(csp, "script-src 'self' 'wasm-unsafe-eval';") ||
		!strings.Contains(csp, "connect-src 'self' https: wss:;") || !strings.Contains(csp, "media-src 'self' blob: https:;") {
		t.Errorf("CSP: %q", csp)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("index cache: %q", w.Header().Get("Cache-Control"))
	}
	a := get(t, h, "/assets/app-123.js", false)
	if a.Code != 200 || a.Body.String() != "console.log(1)" || !strings.Contains(a.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %q", a.Code, a.Body.String(), a.Header().Get("Cache-Control"))
	}
}

func TestUnknownPathsGetTheApp(t *testing.T) {
	h := New(bridge, webDir(t), nil)
	if body := get(t, h, "/some/route", false).Body.String(); !strings.Contains(body, "app") {
		t.Errorf("fallback: %q", body)
	}
	if w := get(t, h, "/assets/missing.js", false); w.Code != http.StatusNotFound {
		t.Errorf("missing asset: %d", w.Code)
	}
}

func TestWithoutWebDirEverythingGoesToTheBridge(t *testing.T) {
	h := New(bridge, "", nil)
	if body := get(t, h, "/", false).Body.String(); body != "bridge" {
		t.Errorf("got %q", body)
	}
}

func TestNoPathTraversal(t *testing.T) {
	dir := webDir(t)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("secret"), 0o644)
	h := New(bridge, dir, nil)
	if body := get(t, h, "/../secret.txt", false).Body.String(); strings.Contains(body, "secret") {
		t.Fatal("path traversal")
	}
}

// On Windows a backslash separates path elements, and path.Clean does not
// know it: "..\" must not reach the file system. Elsewhere a backslash can
// be part of a file's name, which shows here that such paths are refused.
func TestNoBackslashInPaths(t *testing.T) {
	dir := webDir(t)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "private.json"), []byte("secret"), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", `a\b.js`), []byte("secret"), 0o644)
	os.WriteFile(filepath.Join(dir, `a\b.txt`), []byte("secret"), 0o644)
	h := New(bridge, dir, nil)
	for _, p := range []string{"/assets/..%5c..%5cprivate.json", "/assets/a%5cb.js", "/a%5cb.txt", "/..%5cprivate.json"} {
		r := httptest.NewRequest("GET", p, nil)
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "secret") {
			t.Errorf("%s: %d %q", p, w.Code, w.Body.String())
		}
	}
}

func TestAssetsAreGzipped(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	big := bytes.Repeat([]byte("wasm wasm wasm "), 10_000)
	os.WriteFile(filepath.Join(dir, "assets", "app-1.wasm"), big, 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "model-1.tar.gz"), []byte("\x1f\x8balready"), 0o644)
	h := New(http.NotFoundHandler(), dir, nil)
	get := func(path, enc string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if enc != "" {
			r.Header.Set("Accept-Encoding", enc)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := get("/assets/app-1.wasm", "gzip, br")
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Content-Type") != "application/wasm" || w.Body.Len() >= len(big)/10 {
		t.Fatalf("gzip: %v, %d bytes", w.Header(), w.Body.Len())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(zr); !bytes.Equal(got, big) {
		t.Fatal("content differs")
	}
	if w := get("/assets/app-1.wasm", ""); w.Header().Get("Content-Encoding") != "" || w.Body.Len() != len(big) {
		t.Fatalf("plain: %v", w.Header())
	}
	if w := get("/assets/model-1.tar.gz", "gzip"); w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("compressed twice: %v", w.Header())
	}
	if w := get("/assets/missing.js", "gzip"); w.Code != 404 {
		t.Fatalf("missing: %d", w.Code)
	}
}
