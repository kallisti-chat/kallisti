// Package filelink signs file links for the experimental private-room
// media (see files.Handler): the server gives them to a chat's readers in
// its messages, and a link works for a day without a session, as
// Discord's. So the apps' and other servers' media elements, which send no
// cookie, load private files too; a file request checks a signature, not
// the room.
package filelink

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"time"
)

// Life is how long a link works at least.
const Life = 24 * time.Hour

type Signer struct {
	Key []byte
	Now func() time.Time
}

// Query is the signed part of a file's link: ex, when it expires (Unix
// seconds; the hour after Life, so a file's link stays the same for an
// hour and caches keep it), and sig.
func (s Signer) Query(key string) string {
	ex := s.Now().Truncate(time.Hour).Add(Life + time.Hour).Unix()
	return "ex=" + strconv.FormatInt(ex, 10) + "&sig=" + s.sig(key, ex)
}

// Valid: the query signs the file and has not expired.
func (s Signer) Valid(key string, q url.Values) bool {
	ex, err := strconv.ParseInt(q.Get("ex"), 10, 64)
	return err == nil && s.Now().Unix() < ex && hmac.Equal([]byte(q.Get("sig")), []byte(s.sig(key, ex)))
}

func (s Signer) sig(key string, ex int64) string {
	m := hmac.New(sha256.New, s.Key)
	m.Write([]byte(key + "\n" + strconv.FormatInt(ex, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}
