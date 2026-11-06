// Command gen writes models.json: the translation models of the Hugging
// Face mirror of Firefox's models at its current revision, with sizes and
// SHA-256 (see translation.Manifest). Run with go generate.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"kallisti.chat/server/internal/translation"
)

const repo = "midudev/firefox-translations"

func main() {
	res, err := http.Get("https://huggingface.co/api/models/" + repo + "?blobs=true")
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	var info struct {
		SHA      string `json:"sha"`
		Siblings []struct {
			Name string `json:"rfilename"`
			LFS  *struct {
				SHA256 string `json:"sha256"`
				Size   int64  `json:"size"`
			} `json:"lfs"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		log.Fatal(err)
	}
	m := translation.Manifest{Source: "https://huggingface.co/" + repo + "/resolve/" + info.SHA + "/", Pairs: map[string]map[string]translation.File{}}
	for _, s := range info.Siblings {
		pair, name, ok := strings.Cut(s.Name, "/")
		part := translation.Part(name)
		if !ok || s.LFS == nil || part == "" || strings.HasPrefix(pair, "runtime") {
			continue
		}
		if m.Pairs[pair] == nil {
			m.Pairs[pair] = map[string]translation.File{}
		}
		m.Pairs[pair][part] = translation.File{Name: name, Size: s.LFS.Size, SHA256: s.LFS.SHA256}
	}
	out, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("models.json", append(out, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
