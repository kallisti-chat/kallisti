package config

import (
	"strings"
	"testing"
)

func TestOIDCConfig(t *testing.T) {
	base := map[string]string{
		"ALLOWED_ORIGINS":    "https://chat.example",
		"OIDC_ISSUER":        "https://id.example/realm",
		"OIDC_CLIENT_ID":     "kallisti",
		"OIDC_CLIENT_SECRET": "secret-value",
	}
	for _, tt := range []struct {
		name string
		set  map[string]string
		bad  bool
	}{
		{name: "default callback"},
		{name: "secret file", set: map[string]string{"OIDC_CLIENT_SECRET": "", "OIDC_CLIENT_SECRET_FILE": "/run/secrets/oidc"}},
		{name: "missing issuer", set: map[string]string{"OIDC_ISSUER": ""}, bad: true},
		{name: "missing client", set: map[string]string{"OIDC_CLIENT_ID": ""}, bad: true},
		{name: "missing secret", set: map[string]string{"OIDC_CLIENT_SECRET": ""}, bad: true},
		{name: "ambiguous secret", set: map[string]string{"OIDC_CLIENT_SECRET_FILE": "/run/secret"}, bad: true},
		{name: "insecure issuer", set: map[string]string{"OIDC_ISSUER": "http://id.example"}, bad: true},
		{name: "issuer credentials", set: map[string]string{"OIDC_ISSUER": "https://user:secret-value@id.example"}, bad: true},
		{name: "issuer query", set: map[string]string{"OIDC_ISSUER": "https://id.example?token=secret-value"}, bad: true},
		{name: "issuer fragment", set: map[string]string{"OIDC_ISSUER": "https://id.example/#secret-value"}, bad: true},
		{name: "wrong callback origin", set: map[string]string{"OIDC_REDIRECT_URL": "https://evil.example/api/oidc/callback"}, bad: true},
		{name: "wrong callback path", set: map[string]string{"OIDC_REDIRECT_URL": "https://chat.example/callback"}, bad: true},
		{name: "callback query", set: map[string]string{"OIDC_REDIRECT_URL": "https://chat.example/api/oidc/callback?next=evil"}, bad: true},
		{name: "callback fragment", set: map[string]string{"OIDC_REDIRECT_URL": "https://chat.example/api/oidc/callback#evil"}, bad: true},
		{name: "wildcard origin", set: map[string]string{"ALLOWED_ORIGINS": "*", "OIDC_REDIRECT_URL": "https://chat.example/api/oidc/callback"}, bad: true},
		{name: "provider name", set: map[string]string{"OIDC_NAME": "Pocket ID"}},
		{name: "multiline name", set: map[string]string{"OIDC_NAME": "hello\nworld"}, bad: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			vars := make(map[string]string, len(base))
			for k, v := range base {
				vars[k] = v
			}
			for k, v := range tt.set {
				vars[k] = v
			}
			cfg, err := Parse(env(vars))
			if (err != nil) != tt.bad {
				t.Fatalf("unexpected configuration result: %v", err)
			}
			if err != nil {
				if strings.Contains(err.Error(), "secret-value") {
					t.Fatal("configuration error exposed a secret")
				}
				return
			}
			if cfg.OIDC == nil || cfg.OIDC.RedirectURL != "https://chat.example/api/oidc/callback" {
				t.Fatal("missing OIDC configuration or incorrect default callback")
			}
		})
	}
}

func TestOIDCDisabledByDefault(t *testing.T) {
	cfg, err := Parse(env(map[string]string{"ALLOWED_ORIGINS": "*"}))
	if err != nil || cfg.OIDC != nil {
		t.Fatalf("unconfigured OIDC must remain disabled: %v", err)
	}
}
