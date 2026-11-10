package fedi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// NewMiAuthSession returns a fresh UUID v4. MiAuth sessions must never be reused.
func NewMiAuthSession() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// MiAuthURL requests only account information. The caller binds callback to its
// browser state and stores session server-side; callback parameters aren't proof
// of identity. https://misskey-hub.net/en/docs/for-developers/api/token/miauth/
func MiAuthURL(inst Instance, session, name, callback string) string {
	q := url.Values{"name": {name}, "callback": {callback}, "permission": {"read:account"}}
	return inst.Base + "/miauth/" + url.PathEscape(session) + "?" + q.Encode()
}

// postJSON uses the existing address guard, timeout and response size limit.
// Never redirect: MiAuth carries secrets in both URL paths and JSON bodies.
func (c *Client) postJSON(ctx context.Context, u string, body, answer any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil || req.URL.Scheme != "https" && !(c.allowHTTP && req.URL.Scheme == "http") {
		return ErrInstance
	}
	req.Header.Set("Content-Type", "application/json")
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	guarded := *c
	guarded.http = &client
	return guarded.do(req, answer)
}

// IsMisskey detects MiAuth support, including compatible forks, from Misskey's
// detailed metadata. An absent endpoint or capability leaves Mastodon OAuth in
// place; transport, malformed-response and other HTTP failures are errors.
func (c *Client) IsMisskey(ctx context.Context, inst Instance) (bool, error) {
	var meta struct {
		Features struct {
			MiAuth bool `json:"miauth"`
		} `json:"features"`
	}
	err := c.postJSON(ctx, inst.Base+"/api/meta", map[string]bool{"detail": true}, &meta)
	var status *StatusError
	if errors.As(err, &status) && (status.Code == http.StatusNotFound || status.Code == http.StatusMethodNotAllowed || status.Code == http.StatusNotImplemented) {
		return false, nil
	}
	return err == nil && meta.Features.MiAuth, err
}

var miAuthSession = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type misskeyAccount struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	Host      *string `json:"host"`
	AvatarURL string  `json:"avatarUrl"`
}

func (a misskeyAccount) valid() bool {
	return a.Host == nil && remoteID.MatchString(a.ID) && ValidUsername(a.Username)
}

// MiAuthAccount consumes the stored session once, then verifies its token with
// /api/i (read:account). Both answers must identify the same local account.
// The access token is used only for this verification and is never returned.
func (c *Client) MiAuthAccount(ctx context.Context, inst Instance, session string) (Account, error) {
	if !miAuthSession.MatchString(session) {
		return Account{}, fmt.Errorf("%w: invalid MiAuth session", ErrInstance)
	}
	var check struct {
		OK    bool           `json:"ok"`
		Token string         `json:"token"`
		User  misskeyAccount `json:"user"`
	}
	if err := c.postJSON(ctx, inst.Base+"/api/miauth/"+session+"/check", struct{}{}, &check); err != nil {
		return Account{}, err
	}
	if !check.OK || strings.TrimSpace(check.Token) == "" || !check.User.valid() {
		return Account{}, fmt.Errorf("%w: MiAuth was not approved or returned an unexpected account", ErrUnreachable)
	}
	var account misskeyAccount
	if err := c.postJSON(ctx, inst.Base+"/api/i", map[string]string{"i": check.Token}, &account); err != nil {
		return Account{}, err
	}
	if !account.valid() || account.ID != check.User.ID || account.Username != check.User.Username {
		return Account{}, fmt.Errorf("%w: MiAuth account does not match", ErrUnreachable)
	}
	return Account{ID: account.ID, Username: account.Username, Avatar: c.avatarURL(account.AvatarURL)}, nil
}
