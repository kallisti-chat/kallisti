package files

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"kallisti.chat/server/internal/store"
)

func TestMediaAuthorizationCoversEveryRepresentation(t *testing.T) {
	srv, db, h := setupHandler(t, 1<<20)
	body := pngBytes(t)
	_, result := upload(t, srv, "good", "private.png", "image/png", body)
	key := result["key"].(string)
	if err := os.WriteFile(filepath.Join(h.Dir, key+".preview"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetUploadPreview(key, "image/png"); err != nil {
		t.Fatal(err)
	}
	allowed := false
	h.AuthorizeRead = func(_ *http.Request, u store.Upload) bool {
		if u.Key != key {
			t.Fatalf("authorization got wrong upload %q", u.Key)
		}
		return allowed
	}
	for _, query := range []string{"", "?preview", "?download", "?preview&download"} {
		for _, method := range []string{"GET", "HEAD"} {
			for _, header := range []string{"", "Range", "If-Modified-Since", "If-None-Match"} {
				t.Run(method+query+header, func(t *testing.T) {
					request := func() *http.Request {
						r := httptest.NewRequest(method, result["url"].(string)+query, nil)
						switch header {
						case "Range":
							r.Header.Set(header, "bytes=0-9")
						case "If-Modified-Since":
							r.Header.Set(header, h.Now().Format(http.TimeFormat))
						case "If-None-Match":
							r.Header.Set(header, "*")
						}
						return r
					}
					allowed = false
					denied := httptest.NewRecorder()
					h.ServeHTTP(denied, request())
					if denied.Code != 404 || denied.Header().Get("Cache-Control") != "private, no-store" || denied.Header().Get("Content-Range") != "" || bytes.Contains(denied.Body.Bytes(), body) {
						t.Fatalf("denied response: %d %v", denied.Code, denied.Header())
					}
					allowed = true
					got := httptest.NewRecorder()
					h.ServeHTTP(got, request())
					want := 200
					if header == "Range" {
						want = 206
					}
					if header == "If-Modified-Since" || header == "If-None-Match" {
						want = 304
					}
					if got.Code != want || got.Header().Get("Cache-Control") != "private, no-store" {
						t.Fatalf("allowed response: %d %v", got.Code, got.Header())
					}
					if method == "GET" && header == "" && !bytes.Equal(got.Body.Bytes(), body) {
						t.Fatal("allowed media changed")
					}
					if method == "GET" && header == "Range" && !bytes.Equal(got.Body.Bytes(), body[:10]) {
						t.Fatal("range body changed")
					}
					// Membership changes are checked on the very next request.
					allowed = false
					revoked := httptest.NewRecorder()
					h.ServeHTTP(revoked, request())
					if revoked.Code != 404 {
						t.Fatal("revoked reader still has access")
					}
				})
			}
		}
	}
	// Disabling the spike restores the exact legacy read/cache policy.
	h.AuthorizeRead = nil
	legacy := httptest.NewRecorder()
	h.ServeHTTP(legacy, httptest.NewRequest("GET", result["url"].(string), nil))
	if legacy.Code != 200 || legacy.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("legacy response: %d %v", legacy.Code, legacy.Header())
	}
}
