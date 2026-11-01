// Package names holds the user name rules shared by accounts and the bridge.
package names

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Key is the form names are compared in: trimmed (Murmur trims too), NFC
// (composed and decomposed characters are the same name), lower-case.
func Key(name string) string {
	return strings.ToLower(norm.NFC.String(strings.TrimSpace(name)))
}

// Murmur's default "username" rule, without "@": letters, digits and
// -=_[]{}()|. ("name@host" is an account from another server, see Remote).
var allowed = regexp.MustCompile(`^[-=\p{L}\p{N}_\[\]{}()|.]+$`)

// Check returns why a name cannot be used for an account, or "".
func Check(name string) string {
	n := utf8.RuneCountInString(name)
	switch {
	case n < 1 || n > 32:
		return "the name needs 1 to 32 characters"
	case !allowed.MatchString(name):
		return "use letters, digits and - = _ [ ] { } ( ) | . only (no spaces or @)"
	case Key(name) == "superuser":
		return "this name is reserved"
	}
	return ""
}

// AllowedRune: the character may be in a name.
func AllowedRune(r rune) bool { return allowed.MatchString(string(r)) }

// An account from another server (another Kallisti, or the fediverse) is
// named as the fediverse writes it: its name there and its home's host
// (with a port only for local test servers).
var remote = regexp.MustCompile(`^[\p{L}\p{N}_.-]{1,64}@[a-z0-9.-]+(:[0-9]{1,5})?$`)

// MaxRemote: the longest remote name (characters).
const MaxRemote = 128

// CheckRemote returns why a remote account's name is not one, or "".
func CheckRemote(name string) string {
	user, host := Split(name)
	switch {
	case utf8.RuneCountInString(name) > MaxRemote || !remote.MatchString(name):
		return "not a name of an account from another server"
	case strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, ".."):
		return "not a server's host"
	case Key(user) == "superuser":
		return "this name is reserved"
	}
	return ""
}

// Split is a name's own part and its home's host ("": a name of here).
func Split(name string) (user, host string) {
	if i := strings.LastIndex(name, "@"); i > 0 {
		return name[:i], name[i+1:]
	}
	return name, ""
}
