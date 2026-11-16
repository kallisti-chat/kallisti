package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"kallisti.chat/server/internal/auth"
	"kallisti.chat/server/internal/config"
)

func oidcLogin(cfg *config.OIDC) (*auth.OIDCLogin, error) {
	if cfg == nil {
		return nil, nil
	}
	secret := cfg.ClientSecret
	if cfg.ClientSecretFile != "" {
		f, err := os.Open(cfg.ClientSecretFile)
		if err != nil {
			return nil, errors.New("cannot open OIDC_CLIENT_SECRET_FILE")
		}
		defer func() { _ = f.Close() }()
		b, err := io.ReadAll(io.LimitReader(f, 65537))
		if err != nil || len(b) > 65536 {
			return nil, errors.New("cannot read OIDC_CLIENT_SECRET_FILE (maximum 64 KiB)")
		}
		secret = strings.TrimRight(string(b), "\r\n")
	}
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("OIDC client secret is empty")
	}
	login, err := auth.NewOIDCLogin(cfg.Issuer, cfg.ClientID, secret, cfg.RedirectURL, cfg.Name)
	if err != nil {
		return nil, fmt.Errorf("configure OIDC login: %w", err)
	}
	return login, nil
}
