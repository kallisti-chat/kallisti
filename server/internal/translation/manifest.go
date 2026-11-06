// Package translation serves the models for translating messages on the
// reader's device (Bergamot, Firefox's engine; see the web app's
// lib/translate). The server downloads a model file once from a pinned
// mirror of Firefox's models, checks it, and keeps it, so clients get
// models only from here.
package translation

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
)

//go:generate go run ./gen

//go:embed models.json
var bundled []byte

type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest: where the files are (Source + pair + "/" + name) and the files
// of each pair ("zh-en") by part (see Part). LLM: the larger model's files
// (the client's "better translation") by path, at LLMSource + path.
type Manifest struct {
	Source    string                     `json:"source"`
	Pairs     map[string]map[string]File `json:"pairs"`
	LLMSource string                     `json:"llmSource,omitempty"`
	LLM       map[string]File            `json:"llm,omitempty"`
}

// WithoutLLM: the pairs only.
func (m Manifest) WithoutLLM() Manifest {
	m.LLMSource, m.LLM = "", nil
	return m
}

// Part is what a model file is for Bergamot ("": none of its files).
func Part(name string) string {
	for _, p := range []string{"model", "lex", "vocab", "srcvocab", "trgvocab"} {
		if strings.HasPrefix(name, p+".") {
			return p
		}
	}
	return ""
}

// Bundled is the list made by gen.
func Bundled() Manifest {
	var m Manifest
	if err := json.Unmarshal(bundled, &m); err != nil {
		panic(err)
	}
	return m
}

// For: the pairs between English and these languages ("*": all).
func (m Manifest) For(langs []string) Manifest {
	if slices.Contains(langs, "*") {
		return m
	}
	out := Manifest{Source: m.Source, Pairs: map[string]map[string]File{}, LLMSource: m.LLMSource, LLM: m.LLM}
	for pair, files := range m.Pairs {
		other, ok := strings.CutPrefix(pair, "en-")
		if !ok {
			other, _ = strings.CutSuffix(pair, "-en")
		}
		if slices.Contains(langs, other) {
			out.Pairs[pair] = files
		}
	}
	return out
}
