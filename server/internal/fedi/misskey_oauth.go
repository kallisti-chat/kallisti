package fedi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

const misskeyScope = "read:account"

// MisskeyOAuth is discovery metadata bound to the selected instance. Keep it
// with the login flow, including Issuer for RFC 9207 callback validation.
// https://misskey-hub.net/en/docs/for-developers/api/token/oauth/
type MisskeyOAuth struct {
	Issuer                            string `json:"issuer"`
	AuthorizationEndpoint             string `json:"authorization_endpoint"`
	TokenEndpoint                     string `json:"token_endpoint"`
	AuthorizationResponseISSSupported bool   `json:"authorization_response_iss_parameter_supported"`
}

// valid binds discovery to the instance, never to a remote authorization server.
// Issuers have no query/fragment; native Misskey publishes its root as issuer.
func (m *MisskeyOAuth) valid(inst Instance) bool {
	if m == nil || (m.Issuer != inst.Base && m.Issuer != inst.Base+"/") {
		return false
	}
	base, err := url.Parse(inst.Base)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.Path != "" ||
		(base.Scheme != "https" && base.Scheme != "http") {
		return false
	}
	for _, endpoint := range []string{m.Issuer, m.AuthorizationEndpoint, m.TokenEndpoint} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) || u.User != nil ||
			strings.ContainsAny(endpoint, "\\#") || u.Opaque != "" {
			return false
		}
	}
	return true
}

// DiscoverMisskeyOAuth returns nil for an unsupported discovery endpoint. A
// malformed or unsafe answer is an error, not permission to try another flow.
func (c *Client) DiscoverMisskeyOAuth(ctx context.Context, inst Instance) (*MisskeyOAuth, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.Base+"/.well-known/oauth-authorization-server", nil)
	if err != nil {
		return nil, ErrInstance
	}
	var meta struct {
		MisskeyOAuth
		Scopes        []string `json:"scopes_supported"`
		ResponseTypes []string `json:"response_types_supported"`
		GrantTypes    []string `json:"grant_types_supported"`
		Challenges    []string `json:"code_challenge_methods_supported"`
	}
	err = c.doNative(req, &meta)
	var status *StatusError
	if errors.As(err, &status) && (status.Code == http.StatusNotFound || status.Code == http.StatusMethodNotAllowed || status.Code == http.StatusNotImplemented) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !meta.valid(inst) || !slices.Contains(meta.Scopes, misskeyScope) || !slices.Contains(meta.ResponseTypes, "code") ||
		!slices.Contains(meta.Challenges, "S256") || (meta.GrantTypes != nil && !slices.Contains(meta.GrantTypes, "authorization_code")) {
		return nil, fmt.Errorf("%w: unsupported or unsafe Misskey OAuth metadata", ErrUnreachable)
	}
	return &meta.MisskeyOAuth, nil
}

// AuthorizeURL requests only account information using S256 PKCE. The receiver
// must come from DiscoverMisskeyOAuth; state and verifier belong to one flow.
func (m *MisskeyOAuth) AuthorizeURL(clientID, redirectURI, state, challenge string) string {
	u, _ := url.Parse(m.AuthorizationEndpoint)
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", misskeyScope)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String()
}

// doNative keeps the existing address guard, timeout and response size limit,
// but refuses all redirects so codes, verifiers and tokens cannot escape.
func (c *Client) doNative(req *http.Request, answer any) error {
	if req.URL.User != nil || req.URL.Host == "" || req.URL.Scheme != "https" && !(c.allowHTTP && req.URL.Scheme == "http") {
		return ErrInstance
	}
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	guarded := *c
	guarded.http = &client
	return guarded.do(req, answer)
}

func (c *Client) postJSON(ctx context.Context, u string, body, answer any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return ErrInstance
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doNative(req, answer)
}

// MisskeyOAuthAccount exchanges a code as a public client, then reads its local
// account through /api/i. Tokens never leave this function or enter storage.
func (c *Client) MisskeyOAuthAccount(ctx context.Context, inst Instance, meta *MisskeyOAuth, clientID, redirectURI, code, verifier string) (Account, error) {
	if !meta.valid(inst) || code == "" || verifier == "" {
		return Account{}, ErrInstance
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
	}
	if err := c.postJSON(ctx, meta.TokenEndpoint, map[string]string{
		"grant_type": "authorization_code", "client_id": clientID, "redirect_uri": redirectURI,
		"scope": misskeyScope, "code": code, "code_verifier": verifier,
	}, &token); err != nil {
		return Account{}, err
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return Account{}, fmt.Errorf("%w: no access token", ErrUnreachable)
	}
	// Best effort: older servers may not allow token self-revocation. Clean up
	// even if verification fails or the original request has been canceled.
	defer func() {
		_ = c.postJSON(context.WithoutCancel(ctx), inst.Base+"/api/i/revoke-token",
			map[string]string{"i": token.AccessToken, "token": token.AccessToken}, nil)
	}()
	if token.Error != "" || !strings.EqualFold(token.TokenType, "Bearer") || (token.Scope != "" && token.Scope != misskeyScope) {
		return Account{}, fmt.Errorf("%w: unexpected OAuth token response", ErrUnreachable)
	}
	var account struct {
		ID        string  `json:"id"`
		Username  string  `json:"username"`
		Host      *string `json:"host"`
		AvatarURL string  `json:"avatarUrl"`
	}
	if err := c.postJSON(ctx, inst.Base+"/api/i", map[string]string{"i": token.AccessToken}, &account); err != nil {
		return Account{}, err
	}
	if account.Host != nil || !remoteID.MatchString(account.ID) || !ValidUsername(account.Username) {
		return Account{}, fmt.Errorf("%w: unexpected Misskey account", ErrUnreachable)
	}
	return Account{ID: account.ID, Username: account.Username, Avatar: c.avatarURL(account.AvatarURL)}, nil
}
