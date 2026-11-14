package config

import (
	"cmp"
	"errors"
	"net/url"
	"slices"
	"strings"
)

// oidc settings are supplied by the operator, never by login requests.
type OIDC struct {
	Issuer, ClientID, ClientSecret, ClientSecretFile, RedirectURL, Name string
}

func parseOIDC(getenv func(string) string, origins []string) (*OIDC, error) {
	c := &OIDC{
		Issuer: getenv("OIDC_ISSUER"), ClientID: getenv("OIDC_CLIENT_ID"),
		ClientSecret: getenv("OIDC_CLIENT_SECRET"), ClientSecretFile: getenv("OIDC_CLIENT_SECRET_FILE"),
		RedirectURL: getenv("OIDC_REDIRECT_URL"), Name: getenv("OIDC_NAME"),
	}
	if *c == (OIDC{}) {
		return nil, nil
	}
	if c.Issuer == "" || c.ClientID == "" || (c.ClientSecret == "" && c.ClientSecretFile == "") {
		return nil, errors.New("OIDC requires OIDC_ISSUER, OIDC_CLIENT_ID, and OIDC_CLIENT_SECRET or OIDC_CLIENT_SECRET_FILE")
	}
	if c.ClientSecret != "" && c.ClientSecretFile != "" {
		return nil, errors.New("set only one of OIDC_CLIENT_SECRET and OIDC_CLIENT_SECRET_FILE")
	}
	issuer, err := url.Parse(c.Issuer)
	if err != nil || !oidcHTTPSURL(issuer) {
		return nil, errors.New("OIDC_ISSUER must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	if c.RedirectURL == "" {
		for _, origin := range origins {
			if origin != "*" {
				c.RedirectURL = strings.TrimRight(origin, "/") + "/api/oidc/callback"
				break
			}
		}
	}
	redirect, err := url.Parse(c.RedirectURL)
	if err != nil || !oidcHTTPSURL(redirect) || redirect.EscapedPath() != "/api/oidc/callback" || !slices.Contains(origins, redirect.Scheme+"://"+redirect.Host) {
		return nil, errors.New("OIDC_REDIRECT_URL must be https://<allowed-origin>/api/oidc/callback with an exact ALLOWED_ORIGINS entry")
	}
	c.Name = cmp.Or(strings.TrimSpace(c.Name), "OpenID Connect")
	if len(c.Name) > 100 || strings.ContainsAny(c.Name, "\r\n\x00") {
		return nil, errors.New("OIDC_NAME must be a short single-line provider name")
	}
	return c, nil
}

func oidcHTTPSURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !u.OmitHost
}
