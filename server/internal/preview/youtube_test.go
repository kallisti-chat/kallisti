package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

const testVideoID = "abcdefghijk"

func TestYouTubeVideoID(t *testing.T) {
	for _, raw := range []string{
		"https://www.youtube.com/watch?v=" + testVideoID + "&t=30&si=tracking",
		"http://youtube.com/watch?v=" + testVideoID,
		"https://WWW.YouTube.COM:443/watch?v=" + testVideoID,
		"https://m.youtube.com/watch?v=" + testVideoID,
		"https://music.youtube.com/watch?v=" + testVideoID,
		"https://youtu.be/" + testVideoID + "?si=tracking",
		"https://www.youtube.com/shorts/" + testVideoID,
		"https://www.youtube.com/live/" + testVideoID,
		"https://www.youtube.com/embed/" + testVideoID,
		"https://www.youtube-nocookie.com/embed/" + testVideoID,
	} {
		t.Run(raw, func(t *testing.T) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := youtubeVideoID(u); got != testVideoID {
				t.Fatalf("video ID = %q", got)
			}
		})
	}
	for _, raw := range []string{
		"https://youtube.com.evil.test/watch?v=" + testVideoID,
		"https://notyoutube.com/watch?v=" + testVideoID,
		"https://youtube.com@evil.test/watch?v=" + testVideoID,
		"https://user:password@youtube.com/watch?v=" + testVideoID,
		"https://youtube.com:8080/watch?v=" + testVideoID,
		"ftp://youtube.com/watch?v=" + testVideoID,
		"https://youtube.com/playlist?list=" + testVideoID,
		"https://youtube.com/watch?v=short",
		"https://youtube.com/watch?v=" + testVideoID + "extra",
		"https://youtube.com/watch?v=abc!efghijk",
		"https://youtu.be/" + testVideoID + "/extra",
		"https://youtube.com/shorts/" + testVideoID + "/extra",
		"https://youtube.com/watch",
	} {
		t.Run(raw, func(t *testing.T) {
			u, _ := url.Parse(raw)
			if got := youtubeVideoID(u); got != "" {
				t.Fatalf("unexpected video ID %q", got)
			}
		})
	}
	// YouTube IDs can also contain uppercase letters, digits, underscores and hyphens.
	u, _ := url.Parse("https://youtu.be/ABCD_12-xyz")
	if got := youtubeVideoID(u); got != "ABCD_12-xyz" {
		t.Fatalf("valid ID characters rejected: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// All requests stay in this fixture, including those with public-looking URLs.
func fixtureFetcher(handler http.HandlerFunc) *Fetcher {
	f := New()
	f.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler(w, r)
		res := w.Result()
		res.Request = r
		return res, nil
	})
	return f
}

func TestYouTubePreviewUsesOEmbedAndProxiesThumbnail(t *testing.T) {
	var metadata, images atomic.Int32
	f := fixtureFetcher(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Host + r.URL.Path {
		case "www.youtube.com/oembed":
			metadata.Add(1)
			if r.URL.Scheme != "https" || r.URL.Query().Get("url") != "https://www.youtube.com/watch?v="+testVideoID || r.URL.Query().Get("format") != "json" {
				t.Errorf("unexpected provider URL %s", r.URL)
			}
			if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != "Kallisti link preview" {
				t.Errorf("provider headers: %v", r.Header)
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"title":"  A video\n title ","author_name":"The\tchannel","thumbnail_url":"https://i.ytimg.com/vi/abcdefghijk/hqdefault.jpg","provider_name":"ignored","html":"<iframe src='https://evil.test'></iframe>"}`)
		case "i.ytimg.com/vi/abcdefghijk/hqdefault.jpg":
			images.Add(1)
			w.Write(pngOf(16, 9))
		default:
			t.Errorf("unexpected fetch %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	raw := "http://youtu.be/" + testVideoID + "?si=tracking&t=30"
	p := f.Get(context.Background(), raw)
	if p == nil || p.URL != raw || p.Title != "A video title" || p.Description != "The channel" || p.Site != "YouTube" || p.Image == "" || p.ImageWidth != 16 || p.ImageHeight != 9 {
		t.Fatalf("preview %+v", p)
	}
	if again := f.Get(context.Background(), raw); again == nil || metadata.Load() != 1 || images.Load() != 1 {
		t.Fatalf("not cached: metadata=%d images=%d", metadata.Load(), images.Load())
	}
	w := httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest("GET", "/api/link-image/"+p.Image, nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || w.Body.Len() == 0 {
		t.Fatalf("proxied thumbnail: %d %v", w.Code, w.Header())
	}
}

func TestYouTubeMetadataFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"forbidden", `{}`, "application/json", http.StatusForbidden},
		{"rate limited", `{}`, "application/json", http.StatusTooManyRequests},
		{"HTML response", `<title>Bot check</title>`, "text/html", http.StatusOK},
		{"broken JSON", `{`, "application/json", http.StatusOK},
		{"missing title", `{"author_name":"channel"}`, "application/json", http.StatusOK},
		{"blank title", `{"title":" \n\t "}`, "application/json", http.StatusOK},
		{"wrong field type", `{"title":123}`, "application/json", http.StatusOK},
		{"trailing JSON", `{"title":"Title"} {}`, "application/json", http.StatusOK},
		{"oversized JSON", `{"title":"Title"}` + strings.Repeat(" ", 64<<10), "application/json", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureFetcher(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Host != "www.youtube.com" || r.URL.Path != "/oembed" {
					t.Errorf("unexpected fetch %s", r.URL)
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			if p := f.Get(context.Background(), "https://youtu.be/"+testVideoID); p != nil {
				t.Fatalf("unexpected preview %+v", p)
			}
		})
	}
}

func TestYouTubeMetadataTextLimitsAndUnsafeImage(t *testing.T) {
	for _, thumbnail := range []string{"", "javascript:alert(1)", "https://user:password@images.example/pic", ":invalid"} {
		t.Run(thumbnail, func(t *testing.T) {
			f := fixtureFetcher(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/oembed" {
					t.Errorf("unsafe thumbnail requested: %s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"title": strings.Repeat("長", maxTitle+10), "author_name": strings.Repeat("a", maxDescription+10), "thumbnail_url": thumbnail})
			})
			p := f.Get(context.Background(), "https://youtu.be/"+testVideoID)
			if p == nil || len([]rune(p.Title)) != maxTitle+1 || len([]rune(p.Description)) != maxDescription+1 || p.Image != "" {
				t.Fatalf("preview %+v", p)
			}
		})
	}
}

func TestYouTubeThumbnailCannotReachPrivateAddress(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprint("redirect=", redirect), func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Write(pngOf(4, 4))
			}))
			defer srv.Close()
			f := New()
			transport := f.client.Transport
			f.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				switch r.URL.Host + r.URL.Path {
				case "www.youtube.com/oembed":
					thumbnail := srv.URL
					if redirect {
						thumbnail = "https://i.ytimg.com/thumbnail"
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]string{"title": "Title", "thumbnail_url": thumbnail})
				case "i.ytimg.com/thumbnail":
					http.Redirect(w, r, srv.URL, http.StatusFound)
				default:
					if r.URL.String() != srv.URL {
						return nil, fmt.Errorf("unexpected network request %s", r.URL)
					}
					return transport.RoundTrip(r) // real transport must reject loopback
				}
				res := w.Result()
				res.Request = r
				return res, nil
			})
			p := f.Get(context.Background(), "https://youtu.be/"+testVideoID)
			if p == nil || p.Title != "Title" || p.Image != "" || hits.Load() != 0 {
				t.Fatalf("preview %+v; private server hits=%d", p, hits.Load())
			}
		})
	}
}

func TestNonVideoLinksStillUseHTML(t *testing.T) {
	for _, raw := range []string{"https://example.org/article", "https://youtube.com/playlist?list=123", "https://youtube.com.evil.test/watch?v=" + testVideoID} {
		f := fixtureFetcher(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.String() != raw || r.Header.Get("Accept") != "text/html" {
				t.Errorf("unexpected fetch %s", r.URL)
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<title>Ordinary page</title>")
		})
		if p := f.Get(context.Background(), raw); p == nil || p.Title != "Ordinary page" {
			t.Fatalf("%s: %+v", raw, p)
		}
	}
}
