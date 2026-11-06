package preview

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	base, _ := url.Parse("https://example.org/news/1")
	p, img := parse(strings.NewReader(`<!doctype html><html><head>
		<title>Fallback</title>
		<meta property="og:title" content="  The   &amp; Title ">
		<meta property="og:description" content="What it is about.">
		<meta property="og:site_name" content="Example">
		<meta property="og:image" content="/img/cover.png">
		</head><body><meta property="og:title" content="not this"></body></html>`), base)
	if p.Title != "The & Title" || p.Description != "What it is about." || p.Site != "Example" {
		t.Fatalf("%+v", p)
	}
	if img != "https://example.org/img/cover.png" {
		t.Fatalf("image %q", img)
	}
	p, img = parse(strings.NewReader(`<html><head><title>Only a title</title>
		<meta name="description" content="`+strings.Repeat("long ", 200)+`"></head>`), base)
	if p.Title != "Only a title" || len([]rune(p.Description)) > maxDescription+1 || img != "" {
		t.Fatalf("%+v %q", p, img)
	}
	// Only http(s) images.
	if _, img := parse(strings.NewReader(`<meta property="og:image" content="javascript:alert(1)">`), base); img != "" {
		t.Fatalf("image %q", img)
	}
}

func TestPublicAddresses(t *testing.T) {
	for addr, want := range map[string]bool{
		"93.184.216.34:443": true, "[2606:2800:220:1::1]:443": true,
		"93.184.216.34:22": false, "127.0.0.1:443": false, "10.1.2.3:80": false, "192.168.1.1:80": false,
		"172.16.0.1:80": false, "169.254.169.254:80": false, "100.100.1.1:443": false, "0.0.0.0:80": false,
		"[::1]:443": false, "[fd00::1]:443": false, "[fe80::1]:443": false, "[::ffff:127.0.0.1]:443": false,
		"[64:ff9b::a00:1]:443": false, "0.1.2.3:80": false, "240.0.0.1:80": false, "[::7f00:1]:443": false,
		"[2002:a00:1::1]:443": false, "[2001:0:1::1]:443": false, "192.88.99.1:80": false,
	} {
		if got := publicAddr(netip.MustParseAddrPort(addr)); got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}

// local lets tests fetch from httptest (on 127.0.0.1).
func local(netip.AddrPort) bool { return true }

func TestFetchWithImage(t *testing.T) {
	png := pngOf(4, 4)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			hits.Add(1)
			w.Header().Set("Content-Type", "Text/HTML; charset=utf-8")
			fmt.Fprint(w, `<head><title>Hello</title><meta property="og:image" content="/pic"></head>`)
		case "/pic":
			w.Write(png)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/file":
			w.Header().Set("Content-Type", "application/zip")
			fmt.Fprint(w, "PK")
		}
	}))
	defer srv.Close()
	f := New()
	f.allow = local
	p := f.Get(context.Background(), srv.URL+"/page")
	if p == nil || p.Title != "Hello" || p.Image == "" || p.URL != srv.URL+"/page" || p.ImageWidth != 4 || p.ImageHeight != 4 {
		t.Fatalf("preview %+v", p)
	}
	if again := f.Get(context.Background(), srv.URL+"/page"); again == nil || hits.Load() != 1 {
		t.Fatalf("not cached: %d fetches", hits.Load())
	}
	w := httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest("GET", "/api/link-image/"+p.Image, nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Body.Len() != len(png) {
		t.Fatalf("image: %d %v", w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest("GET", "/api/link-image/nope", nil))
	if w.Code != 404 {
		t.Fatalf("unknown image: %d", w.Code)
	}
	for _, path := range []string{"/loop", "/file"} {
		if p := f.Get(context.Background(), srv.URL+path); p != nil {
			t.Fatalf("%s: %+v", path, p)
		}
	}
}

func pngOf(w, h int) []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func TestAVIFImage(t *testing.T) {
	avif, err := os.ReadFile("../mediasize/testdata/grid.avif")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<head><title>Hello</title><meta property="og:image" content="/pic"></head>`)
		case "/pic":
			w.Write(avif)
		}
	}))
	defer srv.Close()
	f := New()
	f.allow = local
	p := f.Get(context.Background(), srv.URL+"/page")
	if p == nil || p.Image == "" || p.ImageWidth != 128 || p.ImageHeight != 96 {
		t.Fatalf("preview %+v", p)
	}
	w := httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest("GET", "/api/link-image/"+p.Image, nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/avif" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("image: %d %v", w.Code, w.Header())
	}
}

// Images their server keeps out of shared caches (a private room's file,
// see files.Handler): a card without, as this cache would share them.
func TestPrivateImagesAreLeftOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pic.png" {
			w.Header().Set("Cache-Control", r.URL.Query().Get("cc"))
			w.Write(pngOf(10, 10))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<head><title>Hello</title><meta property="og:image" content="/pic.png?cc=%s"></head>`, url.QueryEscape(r.URL.Query().Get("cc")))
	}))
	defer srv.Close()
	f := New()
	f.allow = local
	for cc, shown := range map[string]bool{"private, no-store": false, "Private, max-age=60": false, "no-store": false, "public, max-age=60": true, "": true} {
		p := f.Get(context.Background(), srv.URL+"/page?cc="+url.QueryEscape(cc))
		if p == nil || (p.Image != "") != shown {
			t.Errorf("%q: %+v", cc, p)
		}
	}
}

// Images too large to show (by their header) or broken: a card without.
func TestOversizedImagesAreLeftOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/huge", "/broken":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<head><title>Hello</title><meta property="og:image" content="%s.png"></head>`, r.URL.Path)
		case "/huge.png":
			w.Write(pngOf(9000, 9000))
		case "/broken.png":
			w.Write([]byte("\x89PNG\r\n\x1a\n"))
		}
	}))
	defer srv.Close()
	f := New()
	f.allow = local
	for _, path := range []string{"/huge", "/broken"} {
		if p := f.Get(context.Background(), srv.URL+path); p == nil || p.Title != "Hello" || p.Image != "" {
			t.Errorf("%s: %+v", path, p)
		}
	}
}

func TestLocalAddressesAreRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<title>secret admin page</title>`)
	}))
	defer srv.Close()
	if p := New().Get(context.Background(), srv.URL); p != nil {
		t.Fatalf("fetched a local page: %+v", p)
	}
	if p := New().Get(context.Background(), "ftp://example.org/"); p != nil {
		t.Fatalf("ftp: %+v", p)
	}
}

func TestTheQueueIsLimited(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		fmt.Fprint(w, `<title>slow</title>`)
	}))
	defer srv.Close()
	defer close(release)
	f := New()
	f.allow = local
	f.maxQueued = 1
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if p := f.Get(ctx, srv.URL+"/1"); p != nil { // waits, then gives up
		t.Fatalf("got %+v", p)
	}
	if p := f.Get(context.Background(), srv.URL+"/2"); p != nil { // refused at once
		t.Fatalf("got %+v", p)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, queued := f.pages[srv.URL+"/2"]; queued || len(f.pages) != 1 {
		t.Fatalf("pages %v", f.pages)
	}
}

func TestImagesHaveATotalLimit(t *testing.T) {
	f := New()
	f.maxImageBytes = 100
	for i := range 5 {
		f.mu.Lock()
		f.keepImage(fmt.Sprint(i), picture{data: make([]byte, 40), typ: "image/png"})
		f.mu.Unlock()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.images) != 2 || f.imageBytes != 80 {
		t.Fatalf("%d images, %d bytes", len(f.images), f.imageBytes)
	}
	if _, ok := f.images["4"]; !ok {
		t.Fatal("the newest went")
	}
}
