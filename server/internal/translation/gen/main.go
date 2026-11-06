// Command gen writes models.json: the translation models of the Hugging
// Face mirror of Firefox's models at its current revision, and the larger
// model (Hy-MT2, ONNX) at llmRevision, with sizes and SHA-256 (see
// translation.Manifest). Run with go generate.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"kallisti.chat/server/internal/translation"
)

const (
	repo        = "midudev/firefox-translations"
	llmRepo     = "DavidLuong/Hy-MT2-1.8B-ONNX-q4f16"
	llmRevision = "cb415aec769544c3f9cefdc312142b592e731bb9"
)

type sibling struct {
	Name string `json:"rfilename"`
	Size int64  `json:"size"`
	LFS  *struct {
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	} `json:"lfs"`
}

func siblings(api string) (sha string, out []sibling) {
	res, err := http.Get(api)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	var info struct {
		SHA      string    `json:"sha"`
		Siblings []sibling `json:"siblings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		log.Fatal(err)
	}
	return info.SHA, info.Siblings
}

// hashOf downloads a small file (no LFS hash on the Hub) and hashes it.
func hashOf(url string) (string, int64) {
	res, err := http.Get(url)
	if err != nil || res.StatusCode != http.StatusOK {
		log.Fatal(url, err)
	}
	defer res.Body.Close()
	sum := sha256.New()
	n, err := io.Copy(sum, res.Body)
	if err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil)), n
}

func main() {
	sha, files := siblings("https://huggingface.co/api/models/" + repo + "?blobs=true")
	m := translation.Manifest{Source: "https://huggingface.co/" + repo + "/resolve/" + sha + "/", Pairs: map[string]map[string]translation.File{}}
	for _, s := range files {
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
	m.LLMSource = "https://huggingface.co/" + llmRepo + "/resolve/" + llmRevision + "/"
	m.LLM = map[string]translation.File{}
	_, files = siblings("https://huggingface.co/api/models/" + llmRepo + "/revision/" + llmRevision + "?blobs=true")
	for _, s := range files {
		if strings.HasPrefix(s.Name, ".") {
			continue
		}
		f := translation.File{Name: s.Name}
		if s.LFS != nil {
			f.SHA256, f.Size = s.LFS.SHA256, s.LFS.Size
		} else {
			f.SHA256, f.Size = hashOf(m.LLMSource + s.Name)
		}
		m.LLM[s.Name] = f
	}
	out, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("models.json", append(out, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
