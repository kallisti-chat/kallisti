package filelink

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLinksWorkForADay(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 20, 0, 0, time.UTC)
	s := Signer{Key: []byte("k"), Now: func() time.Time { return now }}
	q, err := url.ParseQuery(s.Query("abc"))
	if err != nil {
		t.Fatal(err)
	}
	// Rounded up to the next hour: the same link for an hour (caches).
	if q.Get("ex") != "1793962800" { // 2026-11-06 11:00 UTC
		t.Fatalf("expires %s", q.Get("ex"))
	}
	if again := s.Query("abc"); again != s.Query("abc") || (Signer{Key: []byte("k"), Now: func() time.Time { return now.Add(30 * time.Minute) }}).Query("abc") != again {
		t.Fatal("the link changed within the hour")
	}
	if !s.Valid("abc", q) {
		t.Fatal("not valid")
	}
	other := Signer{Key: []byte("other"), Now: s.Now}
	tampered := url.Values{"ex": {"1893962800"}, "sig": q["sig"]}
	for name, ok := range map[string]bool{
		"other file":    s.Valid("abd", q),
		"other key":     other.Valid("abc", q),
		"later expiry":  s.Valid("abc", tampered),
		"no signature":  s.Valid("abc", url.Values{"ex": q["ex"]}),
		"nothing":       s.Valid("abc", url.Values{}),
		"bad signature": s.Valid("abc", url.Values{"ex": q["ex"], "sig": {strings.Repeat("A", len(q.Get("sig")))}}),
	} {
		if ok {
			t.Errorf("%s: valid", name)
		}
	}
	now = now.Add(24*time.Hour + 39*time.Minute)
	if !s.Valid("abc", q) {
		t.Fatal("expired early")
	}
	now = now.Add(time.Minute)
	if s.Valid("abc", q) {
		t.Fatal("valid after it expired")
	}
}
