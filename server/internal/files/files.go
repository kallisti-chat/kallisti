// Package files handles uploads (POST /api/upload) and serves stored files
// (GET /files/{key}/{name}), and the same for custom emoji (/api/emoji).
package files

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/mediasize"
	"kallisti.chat/server/internal/sidecar"
	"kallisti.chat/server/internal/store"
	"kallisti.chat/server/internal/thumbs"
)

// Only these are shown inline: they cannot carry scripts. SVG can, so it
// is a download like everything else. AVIF: see mediasize.DetectType.
var inlineTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/avif"}

// Pictures few browsers show (JPEG XL): downloads, shown by their preview
// (see previews.go).
var previewedTypes = []string{"image/jxl"}

func isPreviewed(ctype string) bool { return slices.Contains(previewedTypes, ctype) }

// isPicture: an upload of this type is a picture (it needs a size).
func isPicture(ctype string) bool { return slices.Contains(inlineTypes, ctype) || isPreviewed(ctype) }

const (
	// Uploads not attached to a message within this time are deleted, so
	// the server is no free file host.
	UnusedLifetime = 10 * time.Minute
	// A slow upload cannot hold a connection open longer than this.
	uploadTimeout = 2 * time.Minute
)

type Handler struct {
	Store    *store.Store
	Dir      string
	MaxBytes int64
	// Bytes one author may upload in 24 hours.
	QuotaBytes int64
	Now        func() time.Time
	// AuthorizeRead is the experimental private-room media gate. Nil keeps
	// legacy public reads. It runs before opening files or handling ranges
	// and conditionals, including for previews and downloads.
	AuthorizeRead func(*http.Request, store.Upload) bool
	// Redeem checks an upload token and returns the uploader's name.
	Redeem func(token string) (string, bool)
	// RedeemEmoji checks a custom emoji upload token: who adds which
	// emoji. OnEmoji: one was added.
	RedeemEmoji func(token string) (author, name string, ok bool)
	OnEmoji     func()
	// The same for personal emoji: whose and which, and whose changed.
	RedeemUserEmoji func(token string) (user uint32, name string, ok bool)
	OnUserEmoji     func(user uint32)
	// The same for stickers: the sticker a token adds (with its author).
	RedeemSticker func(token string) (store.Sticker, bool)
	OnSticker     func()
	// RedeemLogo checks a logo upload token (admins): who uploads it.
	// OnLogo: the logo changed.
	RedeemLogo func(token string) (author string, ok bool)
	OnLogo     func()
	// The same for a webhook's picture (staff): which webhook's.
	RedeemWebhookPicture func(token string) (webhook int64, ok bool)
	OnWebhookPicture     func(webhook int64)
	// Previews makes previews (nil: none, see previews.go); OnPreview: an
	// upload got one.
	Previews  *thumbs.Maker
	OnPreview func(key string)

	mu       sync.Mutex
	reserved map[string]int64 // by author: bytes of uploads in progress
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	get := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case r.URL.Path == "/api/upload":
		h.post(w, r, h.upload)
	case r.URL.Path == "/api/emoji" && h.RedeemEmoji != nil:
		h.post(w, r, h.uploadEmoji)
	case r.URL.Path == "/api/emoji/personal" && h.RedeemUserEmoji != nil:
		h.post(w, r, h.uploadUserEmoji)
	case r.URL.Path == "/api/stickers" && h.RedeemSticker != nil:
		h.post(w, r, h.uploadSticker)
	case r.URL.Path == "/api/logo" && h.RedeemLogo != nil:
		h.post(w, r, h.uploadLogo)
	case strings.HasPrefix(r.URL.Path, "/api/logo/") && get:
		h.serveLogo(w, r)
	case r.URL.Path == "/api/webhook-picture" && h.RedeemWebhookPicture != nil:
		h.post(w, r, h.uploadWebhookPicture)
	case strings.HasPrefix(r.URL.Path, "/api/webhook-picture/") && get:
		h.serveWebhookPicture(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/stickers/") && get:
		h.serveSticker(w, r)
	case strings.HasPrefix(r.URL.Path, "/files/") && get:
		h.serve(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/emoji/") && get:
		h.serveEmoji(w, r)
	default:
		http.NotFound(w, r)
	}
}

// post answers an upload with CORS headers.
func (h *Handler) post(w http.ResponseWriter, r *http.Request, upload http.HandlerFunc) {
	h.cors(w, r)
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPost:
		config.LimitBody(w) // until the token checks out (then uploadTimeout)
		upload(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// cors lets the web app (possibly on another origin) post uploads.
func (h *Handler) cors(w http.ResponseWriter, r *http.Request) {
	config.CORS(w, r.Header.Get("Origin"), "POST", "Content-Type, X-Filename, X-Upload-Token", false)
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	// In a header, not the URL: URLs end up in proxy logs.
	author, ok := h.Redeem(r.Header.Get("X-Upload-Token"))
	if !ok {
		jsonError(w, "invalid or expired upload token", http.StatusUnauthorized)
		return
	}
	// Without a Content-Length, all that is left may be needed.
	least, most := r.ContentLength, r.ContentLength
	if least <= 0 {
		least, most = 1, h.MaxBytes
	}
	allowed, release, err := h.reserve(author, least, most)
	switch {
	case errors.Is(err, errQuota):
		jsonError(w, "daily upload quota reached, try again tomorrow", http.StatusTooManyRequests)
		return
	case err != nil:
		jsonError(w, "cannot check the upload quota", http.StatusInternalServerError)
		return
	}
	defer release()
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(uploadTimeout))
	key := randomKey()
	dst := filepath.Join(h.Dir, key)
	size, head, err := h.save(dst, r.Body)
	switch {
	case errors.Is(err, errTooLarge):
		jsonError(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	case err != nil:
		slog.Error("save upload", "err", err)
		jsonError(w, "cannot store file", http.StatusInternalServerError)
		return
	case size == 0:
		os.Remove(dst)
		jsonError(w, "empty file", http.StatusBadRequest)
		return
	case size > allowed: // no Content-Length
		os.Remove(dst)
		jsonError(w, "daily upload quota reached, try again tomorrow", http.StatusTooManyRequests)
		return
	}
	name, _ := url.QueryUnescape(r.Header.Get("X-Filename"))
	// The stored type comes from the content, not from the client.
	// Images too large to show (see imagesize; the header can come after
	// the first 512 bytes) have no size: downloads.
	ctype := storedType(head, name)
	media := measure(dst, ctype)
	if isPicture(ctype) && media.Width == 0 {
		ctype = "application/octet-stream"
	}
	u, err := h.Store.AddUpload(store.Upload{Key: key, Name: cleanName(name), Type: ctype, Size: size, Author: author, Media: media}, h.Now())
	if err != nil {
		os.Remove(dst)
		jsonError(w, "cannot store file", http.StatusInternalServerError)
		return
	}
	h.preview(u)
	writeJSON(w, map[string]any{
		"key": u.Key, "name": u.Name, "type": u.Type, "size": u.Size, "url": sidecar.FileURL(u),
	})
}

var (
	errTooLarge = errors.New("too large")
	errQuota    = errors.New("daily upload quota reached")
)

// reserve holds between least and most bytes of an author's daily quota
// (as many as are left) while an upload is stored. Call release once the
// upload is in the database, or failed.
func (h *Handler) reserve(author string, least, most int64) (allowed int64, release func(), err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	used, err := h.Store.UploadedBytesSince(author, h.Now().Add(-24*time.Hour))
	if err != nil {
		return 0, nil, err
	}
	allowed = min(most, h.QuotaBytes-used-h.reserved[author])
	if allowed < least {
		return 0, nil, errQuota
	}
	if h.reserved == nil {
		h.reserved = map[string]int64{}
	}
	h.reserved[author] += allowed
	return allowed, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.reserved[author] -= allowed; h.reserved[author] == 0 {
			delete(h.reserved, author)
		}
	}, nil
}

// save writes at most MaxBytes and returns the size and the first bytes.
func (h *Handler) save(dst string, body io.Reader) (int64, []byte, error) {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, nil, err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(body, head)
	head = head[:n]
	rest, err := io.Copy(f, io.MultiReader(bytes.NewReader(head), io.LimitReader(body, h.MaxBytes+1-int64(n))))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && rest > h.MaxBytes {
		err = errTooLarge
	}
	if err != nil {
		os.Remove(dst)
		return 0, nil, err
	}
	return rest, head, nil
}

// storedType is an upload's type, from its content: a picture, audio or
// video (content and name agree), or a download.
func storedType(head []byte, name string) string {
	if sniffed := mediasize.DetectType(head); isPicture(sniffed) {
		return sniffed
	}
	return cmp.Or(mediaType(head, name), "application/octet-stream")
}

// measure reads a stored file's size and length (see mediasize).
func measure(path, ctype string) store.Media {
	return measureFile(path, func(io.ReaderAt) string { return ctype })
}

// Measure reads an upload's size and length from its file, for uploads
// from before they were kept (see store.MeasureUploads).
func (h *Handler) Measure(u store.Upload) store.Media {
	if !plainKey(u.Key) {
		return store.Media{}
	}
	return measureFile(filepath.Join(h.Dir, u.Key), func(f io.ReaderAt) string { return servedType(u, f) })
}

func measureFile(path string, ctype func(io.ReaderAt) string) store.Media {
	f, err := os.Open(path)
	if err != nil {
		return store.Media{}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return store.Media{}
	}
	return store.Media(mediasize.Probe(f, st.Size(), ctype(f)))
}

// servedType is an upload's type as served: uploads from before media
// types were kept get theirs from the file's start.
func servedType(u store.Upload, f io.ReaderAt) string {
	const download = "application/octet-stream"
	if u.Type != download || !mediaName(u.Name) {
		return u.Type
	}
	head := make([]byte, 512)
	n, _ := f.ReadAt(head, 0)
	return cmp.Or(mediaType(head[:n], u.Name), download)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	if h.AuthorizeRead != nil {
		// Public rooms can become private. Do not let a previously public
		// response bypass the next request's room permission check.
		w.Header().Set("Cache-Control", "private, no-store")
	}
	key, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/files/"), "/")
	u, err := h.Store.UploadByKey(key)
	// ?preview: its preview (see previews.go), if it has one; ?download:
	// the file as an attachment.
	preview := r.URL.Query().Has("preview")
	if err != nil || !plainKey(u.Key) || preview && !u.HasPreview() {
		http.NotFound(w, r)
		return
	}
	if h.AuthorizeRead != nil && !h.AuthorizeRead(r, u) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(h.Dir, u.Key)
	if preview {
		path = previewPath(h.Dir, u.Key)
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	hd := w.Header()
	if preview {
		hd.Set("Content-Type", u.Preview)
	} else {
		disposition := "attachment"
		ctype := "application/octet-stream"
		u.Type = servedType(u, f)
		if isInline(u.Type) {
			disposition, ctype = "inline", u.Type
		}
		if r.URL.Query().Has("download") { // a download button: save, don't show
			disposition = "attachment"
		}
		hd.Set("Content-Type", ctype)
		hd.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": u.Name}))
	}
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// The key is random and the content never changes.
	if h.AuthorizeRead == nil {
		hd.Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, "", u.CreatedAt, f)
}

// cleanName keeps a file name displayable: no path, no control characters,
// at most 200 characters.
func cleanName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	return name
}

func randomKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// DeleteUnused removes uploads that were never attached (database and
// disk). Run it from time to time.
func (h *Handler) DeleteUnused() {
	keys, err := h.Store.DeleteUnusedUploads(h.Now().Add(-UnusedLifetime))
	if err != nil {
		slog.Error("delete unused uploads", "err", err)
		return
	}
	for _, k := range keys {
		h.Remove(k)
	}
}

// Remove deletes an upload's file and preview (its message was deleted).
// Keys are plain names: anything with a path is ignored.
func (h *Handler) Remove(key string) {
	if !plainKey(key) {
		return
	}
	for _, path := range []string{filepath.Join(h.Dir, key), previewPath(h.Dir, key)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("delete upload file", "key", key, "err", err)
		}
	}
}

func plainKey(key string) bool {
	return key != "" && key == filepath.Base(key) && !strings.HasPrefix(key, ".")
}

// Save stores bytes as an upload (for example a picture from a Mumble
// message); attach it to a message to keep it.
// It counts toward the author's daily quota, as uploads from browsers.
func (h *Handler) Save(author, name, ctype string, data []byte) (store.Upload, error) {
	n := int64(len(data))
	_, release, err := h.reserve(author, n, n)
	if err != nil {
		return store.Upload{}, err
	}
	defer release()
	key := randomKey()
	dst := filepath.Join(h.Dir, key)
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return store.Upload{}, err
	}
	media := store.Media(mediasize.Probe(bytes.NewReader(data), n, ctype))
	u, err := h.Store.AddUpload(store.Upload{Key: key, Name: cleanName(name), Type: ctype, Size: n, Author: author, Media: media}, h.Now())
	if err != nil {
		os.Remove(dst)
		return u, err
	}
	h.preview(u)
	return u, nil
}
