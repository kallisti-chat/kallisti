package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"math/bits"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Captcha against scripted sign-ups: when an admin turned it on, the
// browser does some work (a proof of work, no third party) before it
// creates an account or a guest account. A challenge is a random salt and
// its end, signed with a key of this process (nothing stored until used);
// the browser finds a nonce so that SHA-256(challenge + nonce) starts with
// the level's zero bits. Each challenge works once.

const (
	challengeLifetime = 5 * time.Minute
	// Used challenges kept at most (until they expire); more: refused.
	maxUsedChallenges = 100_000
)

// captchaBits: zero bits per level (2^bits tries on average, and 3 times
// that in 5 % of cases). The web client (crypto.subtle in a worker) did
// 340 000 tries a second in Chrome on an Apple M-series laptop; a mid
// laptop does maybe half: normal takes about 1 to 2 s, hard 4 times as
// long. Other levels (and "off"): no challenge.
var captchaBits = map[string]int{"normal": 18, "hard": 20}

type challenges struct {
	once  sync.Once
	key   []byte
	mu    sync.Mutex
	used  map[string]time.Time // payload: its end
	swept time.Time
}

func (c *challenges) mac(payload []byte) []byte {
	c.once.Do(func() {
		c.key = make([]byte, 32)
		rand.Read(c.key)
	})
	m := hmac.New(sha256.New, c.key)
	m.Write(payload)
	return m.Sum(nil)
}

// issue makes a challenge that ends at end.
func (c *challenges) issue(end time.Time) string {
	payload := make([]byte, 24) // salt, end (Unix seconds)
	rand.Read(payload[:16])
	binary.BigEndian.PutUint64(payload[16:], uint64(end.Unix()))
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(c.mac(payload))
}

// use: the nonce solves a challenge of ours that is not used or expired.
// Used ones are kept by payload: the decoder takes other spellings of a
// token too (line breaks), and their keys could be large.
func (c *challenges) use(token, nonce string, need int, now time.Time) bool {
	enc := base64.RawURLEncoding.Strict()
	p, m, _ := strings.Cut(token, ".")
	payload, err1 := enc.DecodeString(p)
	sig, err2 := enc.DecodeString(m)
	if len(token) != 76 || err1 != nil || err2 != nil || len(payload) != 24 || len(nonce) > 64 || !hmac.Equal(sig, c.mac(payload)) {
		return false
	}
	end := time.Unix(int64(binary.BigEndian.Uint64(payload[16:])), 0)
	if sum := sha256.Sum256([]byte(token + nonce)); !now.Before(end) || zeroBits(sum[:]) < need {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used == nil {
		c.used = map[string]time.Time{}
	}
	// Full: drop the expired ones, at most once a minute (not on every
	// request while it stays full).
	if len(c.used) >= maxUsedChallenges && now.Sub(c.swept) >= time.Minute {
		c.swept = now
		for k, e := range c.used {
			if !now.Before(e) {
				delete(c.used, k)
			}
		}
	}
	key := string(payload)
	if _, ok := c.used[key]; ok || len(c.used) >= maxUsedChallenges {
		return false
	}
	c.used[key] = end
	return true
}

// zeroBits counts the leading zero bits.
func zeroBits(b []byte) int {
	n := 0
	for _, x := range b {
		n += bits.LeadingZeros8(x)
		if x != 0 {
			break
		}
	}
	return n
}

func (a *Service) captchaBits() int {
	if a.Captcha == nil {
		return 0
	}
	return captchaBits[a.Captcha()]
}

// challenge answers GET /api/challenge: bits 0 when none is needed.
func (a *Service) challenge(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	need := a.captchaBits()
	if need == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"bits": 0})
		return
	}
	end := a.Now().Add(challengeLifetime).Truncate(time.Second) // as in the token
	writeJSON(w, http.StatusOK, map[string]any{"challenge": a.challenges.issue(end), "bits": need, "expires": end.UnixMilli()})
}

// proven answers the request if it needs a solved challenge and has none.
func (a *Service) proven(w http.ResponseWriter, c credentials) bool {
	need := a.captchaBits()
	if need == 0 || a.challenges.use(c.Challenge, c.Nonce, need, a.Now()) {
		return true
	}
	sendError(w, http.StatusForbidden, "the browser check did not pass, please try again")
	return false
}
