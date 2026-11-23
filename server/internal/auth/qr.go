package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"kallisti.chat/server/internal/store"
)

// Log in with a QR code (see the web app's lib/qrLogin): one device shows
// a code (a link with the token), another scans it, the logged-in one of
// the two confirms, and the other gets a session.
//   - An offer: a logged-in device shows it; a logged-out one (only) scans it
//     (gets a claim secret), the owner confirms (owner secret, its
//     session), the scanner takes its session (wait with the claim).
//   - A request: a logged-out device shows it; a logged-in one scans and
//     confirms (the token, its session), the owner takes its session
//     (wait with the owner secret).
// A code is in memory for qrLifetime, scanned once; its session is taken
// once. Guests neither give nor get one.

const (
	qrLifetime    = 2 * time.Minute
	maxDeviceName = 64
	maxQRPending  = 10_000
)

type qrGrant struct {
	offer   bool
	account store.Account // an offer's owner; a request's scanner
	token   string        // in the code
	owner   string        // the showing device's secret
	claim   string        // an offer's scanner's secret
	device  string        // the showing device's name
	scanner string        // the scanning device's name
	state   string        // waiting, scanned, approved, taking (its session being made), refused, done (an offer's, for its owner)
	voucher string        // the approving session's key: the new one only while it is valid
	expires time.Time
}

// qrGrants keeps grants by each of their keys (token, owner, claim), at
// most maxQRPending at once.
type qrGrants struct {
	mu  sync.Mutex
	by  map[string]*qrGrant
	all map[*qrGrant]bool
}

func (q *qrGrants) add(g *qrGrant, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.by == nil {
		q.by, q.all = map[string]*qrGrant{}, map[*qrGrant]bool{}
	}
	if len(q.all) >= maxQRPending {
		for e := range q.all {
			if !now.Before(e.expires) {
				q.forget(e)
			}
		}
		if len(q.all) >= maxQRPending {
			return false
		}
	}
	q.by[g.token], q.by[g.owner], q.all[g] = g, g, true
	return true
}

// with runs fn on a live grant by one of its keys (and its lock held);
// false: none.
func (q *qrGrants) with(key string, now time.Time, fn func(g *qrGrant)) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	g, ok := q.by[key]
	if !ok || key == "" || !now.Before(g.expires) {
		return false
	}
	fn(g)
	return true
}

// forget drops a grant. With the lock held.
func (q *qrGrants) forget(g *qrGrant) {
	for _, k := range []string{g.token, g.owner, g.claim} {
		delete(q.by, k)
	}
	delete(q.all, g)
}

// deviceName is a device's name as kept (false: empty, too long, or with
// control or format characters).
func deviceName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	// No format characters either (bidi overrides, zero widths): the name
	// is what the other device's question shows.
	return s, n > 0 && n <= maxDeviceName && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) })
}

// readJSON reads a small request body (also unauthenticated ones).
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(v); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

// qrStart makes a code: an offer with a session, else a request.
func (a *Service) qrStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Device string `json:"device"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	device, ok := deviceName(req.Device)
	if !ok {
		sendError(w, http.StatusBadRequest, "a device name has 1 to 64 characters")
		return
	}
	if a.limited(w, r) {
		return
	}
	acc, logged := a.AccountFor(r)
	if logged && acc.Temporary {
		sendError(w, http.StatusForbidden, "set a password first: a guest account cannot log in elsewhere")
		return
	}
	now := a.Now()
	g := &qrGrant{offer: logged, token: randomHex(32), owner: randomHex(32), device: device, state: "waiting", expires: now.Add(qrLifetime)}
	if logged {
		g.account = acc
	}
	if !a.qr.add(g, now) {
		sendError(w, http.StatusServiceUnavailable, "too many codes at once, try again later")
		return
	}
	kind := "request"
	if g.offer {
		kind = "offer"
	}
	w.Header().Set("Cache-Control", "no-store")
	// "code", not "token": clients keep a "token" as their session.
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "code": g.token, "owner": g.owner, "expires": g.expires.UnixMilli()})
}

// qrScan: a device scanned a code (once).
func (a *Service) qrScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token  string `json:"token"`
		Device string `json:"device"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	device, ok := deviceName(req.Device)
	if !ok {
		sendError(w, http.StatusBadRequest, "a device name has 1 to 64 characters")
		return
	}
	acc, logged := a.AccountFor(r)
	status, answer := http.StatusNotFound, map[string]any{"error": "this code is not valid any more"}
	a.qr.with(req.Token, a.Now(), func(g *qrGrant) {
		switch {
		case req.Token != g.token || g.state != "waiting":
		case g.offer && logged:
			// A logged-in device does not change accounts by a code (a
			// stranger's code would move it into the stranger's account);
			// the code stays for the device it is meant for.
			status, answer = http.StatusConflict, map[string]any{"error": "this code logs in a device without an account open: log out here first"}
		case g.offer:
			g.claim, g.scanner, g.state = randomHex(32), device, "scanned"
			a.qr.by[g.claim] = g
			status, answer = http.StatusOK, map[string]any{"kind": "offer", "claim": g.claim, "device": g.device}
		case !logged:
			status, answer = http.StatusUnauthorized, map[string]any{"error": "log in on this device first"}
		case acc.Temporary:
			status, answer = http.StatusForbidden, map[string]any{"error": "set a password first: a guest account cannot log in elsewhere"}
		default:
			g.account, g.scanner, g.state = acc, device, "scanned"
			status, answer = http.StatusOK, map[string]any{"kind": "request", "device": g.device}
		}
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, answer)
}

// qrAnswer: the logged-in device of the two confirms or refuses: an
// offer's owner (its secret), a request's scanner (the token); both with
// the session of the grant's account.
func (a *Service) qrAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Secret string `json:"secret"`
		Yes    bool   `json:"yes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	acc, logged := a.AccountFor(r)
	status := http.StatusNotFound
	a.qr.with(req.Secret, a.Now(), func(g *qrGrant) {
		answers := g.state == "scanned" && (g.offer && req.Secret == g.owner || !g.offer && req.Secret == g.token)
		switch {
		case !answers:
		case !logged || acc.ID != g.account.ID:
			status = http.StatusUnauthorized
		case req.Yes:
			g.state, g.voucher, status = "approved", a.SessionKey(r), http.StatusNoContent
		default:
			g.state, status = "refused", http.StatusNoContent
		}
	})
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, map[string]string{"error": map[int]string{
		http.StatusNotFound:     "this code is not valid any more",
		http.StatusUnauthorized: "log in as this account to answer",
	}[status]})
}

// qrWait: a code's state for one of its devices; the one that logs in
// takes its session here, once, only without a session of its own, and
// only while the approving session is valid (a logout everywhere or a new
// password meanwhile ends it).
func (a *Service) qrWait(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Secret string `json:"secret"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	_, logged := a.AccountFor(r)
	status, answer := http.StatusOK, map[string]any{"state": "expired"}
	var take *qrGrant
	a.qr.with(req.Secret, a.Now(), func(g *qrGrant) {
		owner, claim := req.Secret == g.owner, g.claim != "" && req.Secret == g.claim
		if !owner && !claim {
			return // the token is no secret
		}
		answer = map[string]any{"state": g.state}
		if g.state == "taking" {
			answer["state"] = "approved"
		}
		if owner && g.scanner != "" {
			answer["device"] = g.scanner
		}
		if claim {
			answer["device"] = g.device
		}
		// Whose session: the scanner's of an offer (its owner then sees it
		// done), the owner's of a request.
		if g.state != "approved" || !claim && g.offer {
			return
		}
		if logged {
			status, answer = http.StatusConflict, map[string]any{"error": "log out on this device first"}
			return
		}
		g.state, take = "taking", g // once: no other wait takes it now
	})
	if take == nil {
		writeJSON(w, status, answer)
		return
	}
	token := randomHex(32)
	err := a.Store.CreateSessionVouched(tokenHash(token), take.account.ID, a.Now().Add(sessionLifetime), take.voucher, a.Now())
	// Only then its end shows: an offer's owner sees it done (or refused).
	a.qr.mu.Lock()
	switch {
	case err != nil:
		take.state = "refused"
	case take.offer:
		take.state = "done"
		delete(a.qr.by, take.claim)
		delete(a.qr.by, take.token)
	default:
		a.qr.forget(take)
	}
	a.qr.mu.Unlock()
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"state": "refused"})
		return
	} else if err != nil {
		sendError(w, http.StatusInternalServerError, "cannot log in")
		return
	}
	writeJSON(w, http.StatusOK, withToken(map[string]any{"state": "done", "name": take.account.Name}, a.handOutSession(w, r, token)))
}
