// command preview-init seeds disposable private accounts before any listener starts.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"kallisti.chat/server/internal/certs"
	"kallisti.chat/server/internal/password"
	"kallisti.chat/server/internal/store"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: preview-init DATA_DIRECTORY CREDENTIAL_DIRECTORY")
		os.Exit(2)
	}
	if err := seed(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "preview initialization failed:", err)
		os.Exit(1)
	}
}
func seed(data, credentials string) error {
	marker := filepath.Join(data, ".preview-initialized")
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// validate both files before touching the database. these are preview-only secrets.
	secrets := map[string]string{}
	for _, name := range []string{"lambda", "cortex"} {
		b, err := os.ReadFile(filepath.Join(credentials, name))
		if err != nil {
			return fmt.Errorf("read %s credential: %w", name, err)
		}
		p := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
		if !utf8.ValidString(p) || utf8.RuneCountInString(p) < 16 || utf8.RuneCountInString(p) > 256 {
			return fmt.Errorf("%s credential must contain 16 to 256 characters", name)
		}
		secrets[name] = p
	}
	if err := os.MkdirAll(filepath.Join(data, "tmp"), 0700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(data, ".seed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	dbPath := filepath.Join(tmp, "kallisti.db")
	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, name := range []string{"lambda", "cortex"} {
		hash, err := password.Hash(secrets[name])
		if err != nil {
			return err
		}
		cert, key, err := certs.Generate(name)
		if err != nil {
			return err
		}
		if _, err = db.CreateAccount(store.Account{Name: name, PasswordHash: hash, CertPEM: cert, KeyPEM: key}, time.Now(), ""); err != nil {
			return err
		}
	}
	if err = db.SetInviteOnly(true); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	if _, err = certs.LoadOrCreateServer(tmp); err != nil {
		return err
	}
	// no server runs until this init container completes. an interrupted seed retries
	// from scratch; the marker is written only after every file has been installed.
	for _, name := range []string{"kallisti.db", "cert.pem", "key.pem"} {
		if err = os.Rename(filepath.Join(tmp, name), filepath.Join(data, name)); err != nil {
			return err
		}
	}
	return os.WriteFile(marker, []byte("private preview accounts initialized\n"), 0600)
}
