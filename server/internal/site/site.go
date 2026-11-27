// Package site serves the web app and the bridge from one origin:
// WebSocket upgrades go to the bridge, everything else to the built web app.
package site

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Scripts only from its own origin. Inline styles: Svelte sets style
// attributes. blob: images: upload previews; data: images: avatars (images
// cannot run scripts). wasm-unsafe-eval: compiling WebAssembly (RNNoise),
// not eval of JavaScript. https:/wss: connections, pictures and media:
// other Kallisti servers in the same page (several servers in one client);
// 'self' also allows ws(s) to the same host.
const csp = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; img-src 'self' blob: data: https:; media-src 'self' blob: https:; " +
	"connect-src 'self' https: wss:; style-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

// New returns the site handler. Without webDir, everything goes to the
// bridge (development: the web app runs on Vite). brand: the server's
// name and logo in the page and the manifest (nil: as built).
func New(bridge http.Handler, webDir string, brand func() Brand) http.Handler {
	if webDir == "" {
		return bridge
	}
	files := http.FileServer(http.Dir(webDir))
	gz := &gzipCache{files: map[string]*gzipFile{}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			bridge.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		clean := path.Clean("/" + r.URL.Path)
		if strings.HasPrefix(clean, "/assets/") {
			// Vite puts a content hash in asset names.
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
			if gz.serve(w, r, webDir, clean) {
				return
			}
			files.ServeHTTP(w, r)
			return
		}
		if clean == "/manifest.json" && serveBranded(w, r, webDir, brand, false) {
			return
		}
		// Real files (favicon, ...) as they are; any other path is an app
		// route and gets index.html.
		if clean != "/" && clean != "/index.html" {
			if info, err := os.Stat(filepath.Join(webDir, filepath.FromSlash(clean))); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		if serveBranded(w, r, webDir, brand, true) {
			return
		}
		h.Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
}
