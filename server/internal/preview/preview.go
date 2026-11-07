// Package preview makes link previews: the server fetches a page's title,
// description and image, so readers' browsers never contact the other host.
// Only public addresses on the web ports are fetched (also after DNS and
// redirects), with limits on time and size.
package preview

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"kallisti.chat/server/internal/mediasize"
	"kallisti.chat/server/internal/netguard"
)

const (
	timeout        = 5 * time.Second // per connection step
	fetchTime      = 8 * time.Second // page and image together
	maxRedirects   = 3
	maxPage        = 512 << 10 // bytes read of a page (the head is enough)
	maxImage       = 256 << 10
	maxTitle       = 200 // runes
	maxDescription = 300
	maxSite        = 100
	cacheFor       = time.Hour
	maxEntries     = 500
	parallel       = 4  // fetches at the same time
	maxQueued      = 64 // fetches waiting or running (two pages of links); more get none
	maxImageBytes  = 32 << 20
)

// Preview is what a card shows. Image is the key of the fetched image (see
// ServeHTTP), empty without one; its size in pixels as shown.
type Preview struct {
	URL, Title, Description, Site, Image string
	ImageWidth, ImageHeight              int
}

type picture struct {
	data []byte
	typ  string
	size mediasize.Info
}

type entry struct {
	done    chan struct{}
	preview *Preview // nil: no preview
	at      time.Time
}

// Fetcher fetches and caches previews (also failures, so a link is tried
// once an hour at most).
type Fetcher struct {
	client *http.Client
	allow  func(netip.AddrPort) bool // tests allow local servers
	now    func() time.Time
	sem    chan struct{}
	mu     sync.Mutex
	pages  map[string]*entry
	images map[string]picture
	// Under mu: fetches waiting or running, and image bytes kept (the
	// oldest images go first).
	inflight, maxQueued       int
	imageBytes, maxImageBytes int
	imageOrder                []string
}

func New() *Fetcher {
	f := &Fetcher{allow: publicAddr, now: time.Now, sem: make(chan struct{}, parallel),
		pages: map[string]*entry{}, images: map[string]picture{}, maxQueued: maxQueued, maxImageBytes: maxImageBytes}
	dialer := &net.Dialer{Timeout: timeout, Control: netguard.Control(func(ap netip.AddrPort) bool { return f.allow(ap) })}
	f.client = &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                  nil, // straight to the host: the check above sees its address
			DialContext:            dialer.DialContext,
			DisableKeepAlives:      true, // no idle connections to hosts that keep them open
			TLSHandshakeTimeout:    timeout,
			ResponseHeaderTimeout:  timeout,
			MaxResponseHeaderBytes: 64 << 10,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return errors.New("too many redirects")
			}
			req.Header.Del("Referer") // the link may carry a token
			return webURL(req.URL)
		},
	}
	return f
}

// publicAddr: a public address (see netguard) on the HTTP or HTTPS port.
func publicAddr(ap netip.AddrPort) bool {
	p := ap.Port()
	return (p == 80 || p == 443) && netguard.Public(ap.Addr())
}

func webURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("not a web address")
	}
	return nil
}

// Get returns the preview of a page (nil: none). A fetch of the same page
// that is on its way is waited for.
func (f *Fetcher) Get(ctx context.Context, raw string) *Preview {
	f.mu.Lock()
	e, ok := f.pages[raw]
	if ok && f.now().Sub(e.at) > cacheFor {
		select {
		case <-e.done: // old: fetch again
			f.forget(raw)
			ok = false
		default:
		}
	}
	if !ok {
		if f.inflight >= f.maxQueued {
			f.mu.Unlock()
			return nil // busy: not cached, a later ask may get it
		}
		f.trim()
		f.inflight++
		e = &entry{done: make(chan struct{}), at: f.now()}
		f.pages[raw] = e
		go f.fill(raw, e)
	}
	f.mu.Unlock()
	select {
	case <-e.done:
		return e.preview
	case <-ctx.Done():
		return nil
	}
}

func (f *Fetcher) fill(raw string, e *entry) {
	defer close(e.done)
	f.sem <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTime)
	p, image, err := f.fetch(ctx, raw)
	cancel()
	<-f.sem
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inflight--
	if err != nil {
		slog.Debug("link preview", "url", raw, "err", err)
		return
	}
	if image.data != nil {
		p.Image, p.ImageWidth, p.ImageHeight = randomKey(), image.size.Width, image.size.Height
		f.keepImage(p.Image, image)
	}
	e.preview = &p
}

// keepImage stores an image; the oldest go while over the byte limit.
// Callers hold mu.
func (f *Fetcher) keepImage(key string, img picture) {
	f.images[key] = img
	f.imageBytes += len(img.data)
	f.imageOrder = append(f.imageOrder, key)
	if len(f.imageOrder) > 2*len(f.images)+16 { // keys of images forgotten since
		f.imageOrder = slices.DeleteFunc(f.imageOrder, func(k string) bool { _, ok := f.images[k]; return !ok })
	}
	for f.imageBytes > f.maxImageBytes && len(f.imageOrder) > 0 {
		f.dropImage(f.imageOrder[0])
		f.imageOrder = f.imageOrder[1:]
	}
}

func (f *Fetcher) dropImage(key string) {
	if img, ok := f.images[key]; ok {
		f.imageBytes -= len(img.data)
		delete(f.images, key)
	}
}

func (f *Fetcher) fetch(ctx context.Context, raw string) (Preview, picture, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Preview{}, picture{}, err
	}
	if err := webURL(u); err != nil {
		return Preview{}, picture{}, err
	}
	var p Preview
	var imageURL string
	if id := youtubeVideoID(u); id != "" {
		p, imageURL, err = f.youtube(ctx, id)
	} else {
		p, imageURL, err = f.page(ctx, u.String())
	}
	if err != nil {
		return Preview{}, picture{}, err
	}
	p.URL = raw
	return p, f.image(ctx, imageURL), nil
}

// page fetches and reads a page (its connection closes before the image).
func (f *Fetcher) page(ctx context.Context, u string) (Preview, string, error) {
	res, err := f.get(ctx, u, "text/html")
	if err != nil {
		return Preview{}, "", err
	}
	defer res.Body.Close()
	ctype := res.Header.Get("Content-Type")
	if mt, _, _ := mime.ParseMediaType(ctype); mt != "text/html" && mt != "application/xhtml+xml" {
		return Preview{}, "", errors.New("not a page")
	}
	body, err := charset.NewReader(io.LimitReader(res.Body, maxPage), ctype)
	if err != nil {
		return Preview{}, "", err
	}
	p, imageURL := parse(body, res.Request.URL)
	if p.Title == "" {
		return Preview{}, "", errors.New("no title")
	}
	return p, imageURL, nil
}

func (f *Fetcher) get(ctx context.Context, u, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "Kallisti link preview")
	res, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, errors.New(res.Status)
	}
	return res, nil
}

var imageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/avif"}

// image fetches a preview picture: only real images (by their bytes), and
// small ones (also in pixels: see imagesize; mediasize knows no others).
func (f *Fetcher) image(ctx context.Context, u string) picture {
	if u == "" {
		return picture{}
	}
	res, err := f.get(ctx, u, "image/*")
	if err != nil {
		return picture{}
	}
	defer res.Body.Close()
	if !shareable(res.Header.Get("Cache-Control")) {
		return picture{}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxImage+1))
	if err != nil || len(data) > maxImage {
		return picture{}
	}
	if typ := mediasize.DetectType(data); slices.Contains(imageTypes, typ) {
		if size := mediasize.Probe(bytes.NewReader(data), int64(len(data)), typ); size.Width > 0 {
			return picture{data, typ, size}
		}
	}
	return picture{}
}

// shareable: a response a shared cache may keep (this one is shared by all
// who see the link), not private or no-store.
func shareable(cacheControl string) bool {
	for d := range strings.SplitSeq(strings.ToLower(cacheControl), ",") {
		if d, _, _ := strings.Cut(strings.TrimSpace(d), "="); d == "private" || d == "no-store" {
			return false
		}
	}
	return true
}

// parse reads a page's head: Open Graph tags first, else the title and
// description. imageURL is absolute (http or https), or empty.
func parse(r io.Reader, base *url.URL) (p Preview, imageURL string) {
	meta := map[string]string{}
	var title string
	z := html.NewTokenizer(r)
	for inTitle := false; ; {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			goto done
		case html.TextToken:
			if inTitle && title == "" {
				title = string(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "body":
				goto done
			case "head":
				if tt == html.EndTagToken {
					goto done
				}
			case "title":
				inTitle = tt == html.StartTagToken
			case "meta":
				var key, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "property", "name":
						key = strings.ToLower(string(v))
					case "content":
						content = string(v)
					}
				}
				if _, seen := meta[key]; key != "" && !seen {
					meta[key] = content
				}
			}
		}
	}
done:
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := clean(meta[k]); v != "" {
				return v
			}
		}
		return ""
	}
	p.Title = shorten(cmpOr(first("og:title", "twitter:title"), clean(title)), maxTitle)
	p.Description = shorten(first("og:description", "twitter:description", "description"), maxDescription)
	p.Site = shorten(first("og:site_name"), maxSite)
	if img := first("og:image", "og:image:url", "twitter:image"); img != "" {
		if u, err := base.Parse(img); err == nil && webURL(u) == nil {
			imageURL = u.String()
		}
	}
	return p, imageURL
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// clean makes one line of text: no control characters, single spaces.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n])) + "…"
	}
	return s
}

func randomKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// trim keeps the cache small: when full, the oldest finished entries go
// (with their images). Callers hold mu.
func (f *Fetcher) trim() {
	if len(f.pages) < maxEntries {
		return
	}
	type aged struct {
		raw string
		at  time.Time
	}
	var done []aged
	for raw, e := range f.pages {
		select {
		case <-e.done:
			done = append(done, aged{raw, e.at})
		default: // on its way: kept
		}
	}
	slices.SortFunc(done, func(a, b aged) int { return a.at.Compare(b.at) })
	for _, d := range done[:min(len(done), len(f.pages)-maxEntries*9/10)] {
		f.forget(d.raw)
	}
}

// forget removes a page and its image. Callers hold mu.
func (f *Fetcher) forget(raw string) {
	if e := f.pages[raw]; e != nil && e.preview != nil && e.preview.Image != "" {
		f.dropImage(e.preview.Image)
	}
	delete(f.pages, raw)
}

// ServeHTTP serves fetched images at /api/link-image/<key>.
func (f *Fetcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	img, ok := f.images[strings.TrimPrefix(r.URL.Path, "/api/link-image/")]
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", img.typ)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(img.data))
}
