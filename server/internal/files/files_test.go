package files

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/store"
)

func pngBytes(t *testing.T) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func setup(t *testing.T, maxBytes int64) (*httptest.Server, *store.Store) {
	t.Helper()
	srv, s, _ := setupHandler(t, maxBytes)
	return srv, s
}

func setupHandler(t *testing.T, maxBytes int64) (*httptest.Server, *store.Store, *Handler) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := &Handler{
		Store:      s,
		Dir:        t.TempDir(),
		MaxBytes:   maxBytes,
		QuotaBytes: 3 * maxBytes,
		Now:        func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
		Redeem: func(token string) (string, bool) {
			return "alice", token == "good"
		},
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, s, h
}

func upload(t *testing.T, srv *httptest.Server, token, name, ctype string, body []byte) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+"/api/upload", bytes.NewReader(body))
	req.Header.Set("X-Upload-Token", token)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("X-Filename", url.QueryEscape(name))
	req.Header.Set("Origin", "https://app.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	return resp, v
}

func TestUploadAndServeAnImage(t *testing.T) {
	srv, s := setup(t, 1<<20)
	img := pngBytes(t)
	resp, v := upload(t, srv, "good", "my cat.png", "image/png", img)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d %v", resp.StatusCode, v)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Error("missing CORS header")
	}
	key, _ := v["key"].(string)
	name, _ := v["name"].(string)
	if len(key) < 32 || len(name) != 16 || !strings.HasSuffix(name, ".png") || v["type"] != "image/png" || v["size"] != float64(len(img)) {
		t.Fatalf("response %v", v)
	}
	if _, err := hex.DecodeString(strings.TrimSuffix(name, ".png")); err != nil {
		t.Fatalf("image name is not hexadecimal: %q", name)
	}
	u, err := s.UploadByKey(key)
	if err != nil || u.Author != "alice" || u.Name != name {
		t.Fatalf("stored %+v %v", u, err)
	}
	get, err := http.Get(srv.URL + v["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	data, _ := io.ReadAll(get.Body)
	if get.StatusCode != 200 || !bytes.Equal(data, img) {
		t.Fatalf("serve: %d, %d bytes", get.StatusCode, len(data))
	}
	h := get.Header
	if h.Get("Content-Type") != "image/png" || !strings.HasPrefix(h.Get("Content-Disposition"), "inline") ||
		h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("headers: %v", h)
	}
	// ?download: the same file as an attachment (a download button).
	dl, err := http.Get(srv.URL + v["url"].(string) + "?download")
	if err != nil {
		t.Fatal(err)
	}
	dl.Body.Close()
	if d := dl.Header.Get("Content-Disposition"); !strings.HasPrefix(d, "attachment") || !strings.Contains(d, name) || strings.Contains(d, "my cat") {
		t.Fatalf("download disposition %q", d)
	}
}

func TestImageNamesAreUniqueAndPreserveSpoilers(t *testing.T) {
	srv, _ := setup(t, 1<<20)
	img := pngBytes(t)
	_, first := upload(t, srv, "good", "SPOILER_private.jpg", "image/jpeg", img)
	_, second := upload(t, srv, "good", "SPOILER_private.jpg", "image/jpeg", img)
	a, b := first["name"].(string), second["name"].(string)
	if a == b || !strings.HasPrefix(a, "SPOILER_") || !strings.HasSuffix(a, ".png") || strings.Contains(a, "private") {
		t.Fatalf("names %q and %q", a, b)
	}
	_, file := upload(t, srv, "good", "document.txt", "text/plain", []byte("hello"))
	if file["name"] != "document.txt" {
		t.Fatalf("non-image name changed: %v", file)
	}
}

func TestImageDownloadsDoNotExposeOriginalNames(t *testing.T) {
	for _, tc := range []struct {
		name, ctype, ext string
	}{
		{"IMG_private.HEIC", "application/octet-stream", ".heic"},
		{"private.tiff", "application/octet-stream", ".tiff"},
		{"private.svg", "application/octet-stream", ".svg"},
		{"private.unknown", "image/x-example", ".bin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := setup(t, 1<<20)
			resp, v := upload(t, srv, "good", tc.name, tc.ctype, []byte("sample image data"))
			got, _ := v["name"].(string)
			if resp.StatusCode != http.StatusOK || len(got) != 12+len(tc.ext) || strings.Contains(got, "private") || !strings.HasSuffix(got, tc.ext) || v["type"] != "application/octet-stream" {
				t.Fatalf("%s: %d %v", tc.name, resp.StatusCode, v)
			}
		})
	}
}

func TestNonImagesAreDownloadsEvenIfTheyClaimOtherwise(t *testing.T) {
	srv, _ := setup(t, 1<<20)
	html := []byte("<html><script>alert(1)</script></html>")
	for _, ctype := range []string{"text/html", "image/png", "image/svg+xml"} {
		_, v := upload(t, srv, "good", "x.html", ctype, html)
		get, err := http.Get(srv.URL + v["url"].(string))
		if err != nil {
			t.Fatal(err)
		}
		get.Body.Close()
		if get.Header.Get("Content-Type") != "application/octet-stream" ||
			!strings.HasPrefix(get.Header.Get("Content-Disposition"), "attachment") {
			t.Errorf("%s: served as %q, %q", ctype, get.Header.Get("Content-Type"), get.Header.Get("Content-Disposition"))
		}
	}
}

// Images too large to show inline (by their header; a JPEG's size can come
// after the first 512 bytes) are kept, as downloads.
func TestOversizedImagesAreDownloads(t *testing.T) {
	srv, _ := setup(t, 1<<20)
	var j bytes.Buffer
	if err := jpeg.Encode(&j, image.NewGray(image.Rect(0, 0, 20000, 1)), nil); err != nil {
		t.Fatal(err)
	}
	app := append([]byte{0xff, 0xe1, 0x04, 0x02}, make([]byte, 0x400)...) // 1 KB of metadata
	bigJPEG := slices.Concat(j.Bytes()[:2], app, j.Bytes()[2:])
	var p bytes.Buffer
	png.Encode(&p, image.NewGray(image.Rect(0, 0, 9000, 9000)))
	for name, data := range map[string][]byte{"wide.jpg": bigJPEG, "huge.png": p.Bytes()} {
		resp, v := upload(t, srv, "good", name, "image/jpeg", data)
		if resp.StatusCode != 200 || v["type"] != "application/octet-stream" {
			t.Errorf("%s: %d %v", name, resp.StatusCode, v)
			continue
		}
		get, err := http.Get(srv.URL + v["url"].(string))
		if err != nil {
			t.Fatal(err)
		}
		get.Body.Close()
		if !strings.HasPrefix(get.Header.Get("Content-Disposition"), "attachment") {
			t.Errorf("%s: served %q", name, get.Header.Get("Content-Disposition"))
		}
	}
}

func TestUploadRefusals(t *testing.T) {
	srv, _ := setup(t, 100)
	if resp, _ := upload(t, srv, "bad", "a.txt", "text/plain", []byte("x")); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token: %d", resp.StatusCode)
	}
	if resp, _ := upload(t, srv, "good", "a.bin", "application/octet-stream", make([]byte, 101)); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", resp.StatusCode)
	}
	if resp, _ := upload(t, srv, "good", "empty.txt", "text/plain", nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty: %d", resp.StatusCode)
	}
}

func TestUnknownFileIs404(t *testing.T) {
	srv, _ := setup(t, 100)
	resp, err := http.Get(srv.URL + "/files/nope/x.png")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestPreflight(t *testing.T) {
	srv, _ := setup(t, 100)
	req, _ := http.NewRequest("OPTIONS", srv.URL+"/api/upload", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "X-Upload-Token") {
		t.Fatalf("preflight: %d %v", resp.StatusCode, resp.Header)
	}
	// Other sites (another Kallisti's page) upload with their token too.
	req.Header.Set("Origin", "https://other-kallisti.example")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "https://other-kallisti.example" {
		t.Error("no CORS for another site")
	}
}

// A refused upload whose body never ends gets its answer after
// config.BodyTimeout: the unread rest is not awaited forever.
func TestRefusedStalledUploadsEnd(t *testing.T) {
	srv, _ := setup(t, 1<<20)
	defer func(d time.Duration) { config.BodyTimeout = d }(config.BodyTimeout)
	config.BodyTimeout = 100 * time.Millisecond
	body, pw := io.Pipe()
	defer pw.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/upload", body)
	req.ContentLength = 1000
	req.Header.Set("X-Upload-Token", "bad")
	done := make(chan int, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- 0
			return
		}
		resp.Body.Close()
		done <- resp.StatusCode
	}()
	io.WriteString(pw, "abc")
	select {
	case code := <-done:
		if code != http.StatusUnauthorized {
			t.Fatalf("status %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the refused upload is still read")
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":       "passwd",
		"a\x00b\nc.txt":          "abc.txt",
		"":                       "file",
		strings.Repeat("x", 300): strings.Repeat("x", 200),
	}
	for in, want := range cases {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeleteUnusedRemovesFiles(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	h := &Handler{Store: s, Dir: t.TempDir(), MaxBytes: 100, QuotaBytes: 1000, Now: func() time.Time { return now },
		Redeem: func(string) (string, bool) { return "alice", true }}
	srv := httptest.NewServer(h)
	defer srv.Close()
	_, v := upload(t, srv, "any", "a.txt", "text/plain", []byte("hello"))
	key := v["key"].(string)
	now = now.Add(UnusedLifetime + time.Second)
	h.DeleteUnused()
	if _, err := s.UploadByKey(key); err == nil {
		t.Fatal("upload still in the database")
	}
	if entries, _ := os.ReadDir(h.Dir); len(entries) != 0 {
		t.Fatalf("files left: %v", entries)
	}
}

func TestDailyQuota(t *testing.T) {
	srv, _ := setup(t, 100) // quota: 300 bytes per day
	for i := 0; i < 3; i++ {
		if resp, v := upload(t, srv, "good", "a.bin", "application/octet-stream", make([]byte, 100)); resp.StatusCode != http.StatusOK {
			t.Fatalf("upload %d: %d %v", i, resp.StatusCode, v)
		}
	}
	resp, v := upload(t, srv, "good", "a.bin", "application/octet-stream", make([]byte, 10))
	if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(fmt.Sprint(v["error"]), "quota") {
		t.Fatalf("over quota: %d %v", resp.StatusCode, v)
	}
}

// quotaHandler allows 100 bytes per file and per day.
func quotaHandler(t *testing.T) *Handler {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Handler{Store: s, Dir: t.TempDir(), MaxBytes: 100, QuotaBytes: 100, Now: time.Now,
		Redeem: func(string) (string, bool) { return "alice", true }}
}

// serveUpload posts a body to the handler (size -1: no Content-Length).
func serveUpload(h *Handler, body io.Reader, size int64) int {
	req := httptest.NewRequest("POST", "/api/upload", body)
	req.ContentLength = size
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestParallelUploadsKeepTheDailyQuota(t *testing.T) {
	h := quotaHandler(t)
	pr, pw := io.Pipe()
	first := make(chan int)
	go func() { first <- serveUpload(h, pr, 60) }()
	pw.Write(make([]byte, 30)) // the first upload is past the quota check
	if code := serveUpload(h, bytes.NewReader(make([]byte, 60)), 60); code != http.StatusTooManyRequests {
		t.Errorf("second upload: %d", code)
	}
	if _, err := h.Save("alice", "a.png", "image/png", make([]byte, 60)); err == nil {
		t.Error("saved over the quota")
	}
	pw.Write(make([]byte, 30))
	pw.Close()
	if code := <-first; code != http.StatusOK {
		t.Fatalf("first upload: %d", code)
	}
}

func TestFailedUploadsFreeTheirQuota(t *testing.T) {
	h := quotaHandler(t)
	if code := serveUpload(h, bytes.NewReader(make([]byte, 150)), -1); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large: %d", code)
	}
	if code := serveUpload(h, bytes.NewReader(make([]byte, 60)), 60); code != http.StatusOK {
		t.Fatalf("after a failed upload: %d", code)
	}
	if code := serveUpload(h, bytes.NewReader(make([]byte, 60)), -1); code != http.StatusTooManyRequests {
		t.Fatalf("over the quota: %d", code)
	}
}

func TestTokenInQueryIsNotAccepted(t *testing.T) {
	srv, _ := setup(t, 100)
	resp, err := http.Post(srv.URL+"/api/upload?token=good", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

func TestRemoveDeletesOnlyAnUploadFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "abc123"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "outside"), []byte("x"), 0o600)
	h := &Handler{Dir: dir}
	h.Remove("abc123")
	h.Remove("../outside")
	if _, err := os.Stat(filepath.Join(dir, "abc123")); !os.IsNotExist(err) {
		t.Fatal("file still there")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "outside")); err != nil {
		t.Fatal("removed a file outside the upload folder")
	}
}

func TestSaveStoresBytesAsAnUpload(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	dir := t.TempDir()
	h := &Handler{Store: s, Dir: dir, Now: time.Now, QuotaBytes: 1 << 20}
	u, err := h.Save("Jukebox", "cover.jpg", "image/jpeg", []byte("\xff\xd8\xffjpeg"))
	if err != nil || u.Type != "image/jpeg" || u.Author != "Jukebox" || u.Size != 7 {
		t.Fatalf("upload %+v %v", u, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, u.Key)); err != nil || string(data) != "\xff\xd8\xffjpeg" {
		t.Fatalf("file %q %v", data, err)
	}
}

func TestSaveKeepsTheDailyQuota(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	h := &Handler{Store: s, Dir: t.TempDir(), Now: time.Now, QuotaBytes: 10}
	if _, err := h.Save("bob", "a.png", "image/png", make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Save("bob", "b.png", "image/png", make([]byte, 8)); err == nil {
		t.Fatal("over the quota")
	}
}
