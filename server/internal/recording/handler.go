package recording

import (
	"archive/zip"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func stamp(info Info) string { return info.Started.UTC().Format("2006-01-02 1504") }

// zipName: the download's file name, by channel and start time (UTC).
func zipName(info Info) string { return safeName(info.ChannelName) + " " + stamp(info) + ".zip" }

// WriteZip writes a finished recording as a zip (stored: Opus does not
// shrink). Tracks are named by the start time and the speaker.
func (l *Library) WriteZip(w io.Writer, id string) error {
	info, err := l.info(id)
	if err != nil {
		return err
	}
	z := zip.NewWriter(w)
	add := func(name, path string) error {
		f, err := os.Open(path) // #nosec G304 G703 -- a validID (l.info above) and a base name
		if err != nil {
			return err
		}
		defer f.Close()
		out, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: info.Stopped})
		if err == nil {
			_, err = io.Copy(out, f)
		}
		return err
	}
	for _, t := range info.Tracks {
		if err := add(stamp(info)+" "+t.File, filepath.Join(l.Dir, id, filepath.Base(t.File))); err != nil {
			return err
		}
	}
	if err := add(infoFile, filepath.Join(l.Dir, id, infoFile)); err != nil {
		return err
	}
	return z.Close()
}

// Handler serves GET /api/recordings/{id}?token=: a recording's zip, for a
// download token (single use, from the sidecar) of a user who may still
// download recordings. The token is in the URL: a download is a plain
// link, which cannot send headers.
type Handler struct {
	Library     *Library
	Redeem      func(token string) (session uint32, ok bool)
	MayDownload func(session uint32) bool
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, ok := h.Redeem(r.URL.Query().Get("token"))
	if !ok {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	if !h.MayDownload(session) {
		http.Error(w, "only admins and moderators download recordings", http.StatusForbidden)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/recordings/")
	info, ok := h.Library.finished(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": zipName(info)}))
	w.Header().Set("Cache-Control", "no-store")
	if err := h.Library.WriteZip(w, id); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Error("recording: download", "id", id, "err", err)
	}
}
