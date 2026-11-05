package store

import (
	"bytes"
	"path/filepath"
	"testing"
)

// A secret is made once and kept, also after a restart.
func TestSecretsAreKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Secret("file_links")
	if err != nil || len(a) != 32 {
		t.Fatalf("%x %v", a, err)
	}
	other, _ := s.Secret("other")
	if bytes.Equal(a, other) {
		t.Fatal("two names, one secret")
	}
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if b, err := s.Secret("file_links"); err != nil || !bytes.Equal(a, b) {
		t.Fatalf("after a restart: %x %v", b, err)
	}
}
