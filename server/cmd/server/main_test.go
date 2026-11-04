package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"kallisti.chat/server/internal/config"
)

func TestConfigurationWarnings(t *testing.T) {
	for _, tc := range []struct {
		name, origins, trust string
		proxy, wildcard      bool
	}{
		{"https without trust", "https://chat.example", "", true, false},
		{"https with disabled trust", "https://chat.example", "0", true, false},
		{"trusted https", "https://chat.example", "1", false, false},
		{"plain localhost", "http://localhost:5173", "", false, false},
		{"wildcard", "*", "", false, true},
		{"trusted wildcard", "*", "1", false, true},
		{"https after http", "http://localhost:5173, https://chat.example", "", true, false},
		{"multiple https origins", "https://chat.example,https://other.example", "", true, false},
		{"wildcard and https", "*,https://chat.example", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse(func(key string) string {
				switch key {
				case "ALLOWED_ORIGINS":
					return tc.origins
				case "TRUST_FORWARDED_FOR":
					return tc.trust
				}
				return ""
			})
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			warnConfig(cfg, slog.New(slog.NewTextHandler(&buf, nil)))
			out := buf.String()
			if strings.Contains(out, "X-Forwarded-Proto") != tc.proxy || strings.Contains(out, "development only") != tc.wildcard {
				t.Fatalf("proxy warning %v, wildcard warning %v: %s", tc.proxy, tc.wildcard, out)
			}
			wantWarnings := 0
			if tc.proxy {
				wantWarnings++
				for _, detail := range []string{"TRUST_FORWARDED_FOR", "Secure", "__Host-", "direct access"} {
					if !strings.Contains(out, detail) {
						t.Errorf("warning missing %q: %s", detail, out)
					}
				}
			}
			if tc.wildcard {
				wantWarnings++
			}
			if got := strings.Count(out, "level=WARN"); got != wantWarnings {
				t.Errorf("got %d warnings, want %d: %s", got, wantWarnings, out)
			}
		})
	}
}
