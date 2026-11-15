package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kallisti.chat/server/internal/config"
)

func TestOIDCSecretFile(t *testing.T) {
	for _, tc := range []struct {
		name, contents string
		missing, bad   bool
	}{
		{name: "secret", contents: "test-client-secret\n"},
		{name: "empty", bad: true},
		{name: "newline", contents: "\n", bad: true},
		{name: "too large", contents: strings.Repeat("s", 65537), bad: true},
		{name: "missing", missing: true, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secret")
			if !tc.missing {
				if err := os.WriteFile(path, []byte(tc.contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c := &config.OIDC{Issuer: "https://identity.invalid", ClientID: "client", ClientSecretFile: path, RedirectURL: "https://chat.example/api/oidc/callback", Name: "Test"}
			login, err := oidcLogin(c)
			if (err != nil) != tc.bad {
				t.Fatalf("unexpected secret file result: %v", err)
			}
			if !tc.bad && login == nil {
				t.Fatal("provider was not configured")
			}
			if err != nil && strings.Contains(err.Error(), "test-client-secret") {
				t.Fatal("error exposed secret")
			}
		})
	}
}

func TestOIDCDisabledStartup(t *testing.T) {
	login, err := oidcLogin(nil)
	if login != nil || err != nil {
		t.Fatal("disabled OIDC altered startup")
	}
}
