package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kallisti.chat/server/internal/password"
	"kallisti.chat/server/internal/store"
)

func TestSeedPrivateAccountsAndRetry(t *testing.T) {
	data, secrets := t.TempDir(), t.TempDir()
	for _, name := range []string{"lambda", "cortex"} {
		if err := os.WriteFile(filepath.Join(secrets, name), []byte("preview-only-test-password\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := seed(data, secrets); err != nil {
		t.Fatal(err)
	}
	if err := seed(data, secrets); err != nil {
		t.Fatal("init restart:", err)
	}
	db, err := store.Open(filepath.Join(data, "kallisti.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	on, err := db.InviteOnly()
	if err != nil || !on {
		t.Fatalf("registration open: %v", err)
	}
	for _, name := range []string{"lambda", "cortex"} {
		a, err := db.AccountByName(name)
		if err != nil {
			t.Fatal(err)
		}
		if a.Temporary || !password.Check(a.PasswordHash, "preview-only-test-password") || len(a.CertPEM) == 0 {
			t.Fatalf("invalid private account %s", name)
		}
	}
	for _, temporary := range []bool{false, true} {
		_, err := db.CreateAccount(store.Account{Name: "outsider", Temporary: temporary}, time.Now(), "")
		if !errors.Is(err, store.ErrRegistrationInvite) {
			t.Fatalf("uninvited account allowed: %v", err)
		}
	}
}
func TestInvalidCredentialsFailBeforeDBCreation(t *testing.T) {
	data, secrets := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(secrets, "lambda"), []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if seed(data, secrets) == nil {
		t.Fatal("invalid password accepted")
	}
	if _, err := os.Stat(filepath.Join(data, "kallisti.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("database created before credentials validated")
	}
}
