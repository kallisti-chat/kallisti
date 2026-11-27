package site

import (
	"encoding/json"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Brand is the server's name and its logo's version ("": none; the
// icons are at /api/logo/<size>.png?v=<version>), set by admins.
type Brand struct {
	Name string
	Logo string
}

func logoURL(size, version string) string { return "/api/logo/" + size + ".png?v=" + version }

// LogoIcon is the logo's icon for notifications ("": no logo).
func LogoIcon(version string) string {
	if version == "" {
		return ""
	}
	return logoURL("192", version)
}

// brandIndex puts the name in the page's title and the logo in its icons
// (index.html as Vite builds it: other pages stay as they are).
func brandIndex(page string, b Brand) string {
	if b.Name != "" {
		page = strings.Replace(page, "<title>Kallisti</title>", "<title>"+html.EscapeString(b.Name)+"</title>", 1)
	}
	if b.Logo != "" {
		page = strings.Replace(page, `href="/icons/favicon-32.png"`, `href="`+logoURL("32", b.Logo)+`"`, 1)
		page = strings.Replace(page, `href="/icons/apple-touch-icon.png"`, `href="`+logoURL("180", b.Logo)+`"`, 1)
	}
	return page
}

// brandManifest does the same for the installed app: its name and icons.
func brandManifest(raw []byte, b Brand) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if b.Name != "" {
		m["name"], m["short_name"] = b.Name, b.Name
	}
	if b.Logo != "" {
		icon := func(size string) map[string]string {
			return map[string]string{"src": logoURL(size, b.Logo), "sizes": size + "x" + size, "type": "image/png"}
		}
		m["icons"] = []map[string]string{icon("192"), icon("512")}
	}
	return json.Marshal(m)
}

// serveBranded serves index.html or manifest.json with the brand (false:
// not one of them, or no brand).
func serveBranded(w http.ResponseWriter, r *http.Request, webDir string, brand func() Brand, index bool) bool {
	if brand == nil {
		return false
	}
	name, ctype := "index.html", "text/html; charset=utf-8"
	if !index {
		name, ctype = "manifest.json", "application/manifest+json"
	}
	raw, err := os.ReadFile(filepath.Join(webDir, name)) // #nosec G304 -- a fixed name
	if err != nil {
		return false
	}
	b := brand()
	out := []byte(brandIndex(string(raw), b))
	if !index {
		if out, err = brandManifest(raw, b); err != nil {
			return false
		}
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method != http.MethodHead {
		w.Write(out)
	}
	return true
}
