package preview

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/url"
	"strings"
)

const maxYouTubeMetadata = 64 << 10

// youtubeVideoID recognizes video links, not arbitrary URLs or channel pages.
// Only the ID goes to the provider; tracking parameters and timestamps stay out.
func youtubeVideoID(u *url.URL) string {
	if webURL(u) != nil || u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80") {
		return ""
	}
	var id string
	switch strings.ToLower(u.Hostname()) {
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com", "www.youtube-nocookie.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else if parts := strings.Split(u.Path, "/"); len(parts) == 3 {
			switch parts[1] {
			case "shorts", "live", "embed":
				id = parts[2]
			}
		}
	}
	if len(id) != 11 {
		return ""
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return ""
		}
	}
	return id
}

// youtube gets metadata without the watch page, cookies, API keys or embed HTML.
// The fixed endpoint and the usual guarded client keep provider lookup bounded.
func (f *Fetcher) youtube(ctx context.Context, id string) (Preview, string, error) {
	query := url.Values{"url": {"https://www.youtube.com/watch?v=" + id}, "format": {"json"}}
	res, err := f.get(ctx, "https://www.youtube.com/oembed?"+query.Encode(), "application/json")
	if err != nil {
		return Preview{}, "", err
	}
	defer res.Body.Close()
	if mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type")); mt != "application/json" && mt != "application/json+oembed" {
		return Preview{}, "", errors.New("not YouTube metadata JSON")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxYouTubeMetadata+1))
	if err != nil {
		return Preview{}, "", err
	}
	if len(body) > maxYouTubeMetadata {
		return Preview{}, "", errors.New("YouTube metadata too large")
	}
	var data struct {
		Title     string `json:"title"`
		Author    string `json:"author_name"`
		Thumbnail string `json:"thumbnail_url"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Preview{}, "", err
	}
	p := Preview{Title: shorten(clean(data.Title), maxTitle), Description: shorten(clean(data.Author), maxDescription), Site: "YouTube"}
	if p.Title == "" {
		return Preview{}, "", errors.New("no title")
	}
	var imageURL string
	if u, err := url.Parse(data.Thumbnail); err == nil && webURL(u) == nil {
		imageURL = u.String()
	}
	return p, imageURL, nil
}
