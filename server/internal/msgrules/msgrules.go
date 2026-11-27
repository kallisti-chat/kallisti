// Package msgrules has the server's message rules: blocked words and link
// domains (for everyone but staff), and limits for new accounts and guests.
// Pure functions: the built-in server keeps the rules, the registration
// times and the last message times.
package msgrules

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"
)

// Rules are saved by the server and changed by admins.
type Rules struct {
	// Accounts younger than this (and guests) are limited; 0: off.
	NewMinutes int      `json:"newMinutes"`
	Words      []string `json:"words"`   // words and phrases
	Domains    []string `json:"domains"` // with their subdomains
	// Phrases posted as one of their replacements.
	Replacements []Replacement `json:"replacements"`
}

// Replacement: a phrase (matched as blocked words) and what may replace it:
// plain text, without what messages format, link or mention.
type Replacement struct {
	Phrase string   `json:"phrase"`
	With   []string `json:"with"`
}

const (
	MaxNewMinutes = 7 * 24 * 60
	maxEntries    = 500
	maxEntry      = 100 // characters
	maxWith       = 20  // replacements of a phrase
	// Limited authors send one message per Interval.
	Interval = 5 * time.Second
)

// Author is who writes.
type Author struct {
	Staff bool
	Guest bool // no account, or a guest account
	// The registration; older than NewMinutes (or zero: from before the
	// rules) is not new.
	Since time.Time
}

// Message is what an author wants to post.
type Message struct {
	Text       string
	Upload     bool // a file or picture
	MentionAll bool // @everyone or @here
	Post       bool // a new message (edits and uploads have no rate)
}

var (
	ErrWord   = errors.New("This message contains a blocked word")
	ErrDomain = errors.New("This message links to a blocked site")
)

// Limited: the author is new or a guest while the limits are on.
func Limited(r Rules, a Author, now time.Time) bool {
	return r.NewMinutes > 0 && !a.Staff && (a.Guest || now.Sub(a.Since) < time.Duration(r.NewMinutes)*time.Minute)
}

// Compiled are rules ready for matching: the words and phrases folded,
// the domains in their ASCII form. That is most of the work of a check, so
// the server compiles the rules when they change, not for each message.
// Never changed after Compile: it may be shared.
type Compiled struct {
	newMinutes int
	words      []entry
	phrases    []entry
	with       [][]string // of each phrase
	domains    []string   // only those that are domains
}

// Compile makes the rules ready for matching.
func Compile(r Rules) *Compiled {
	c := &Compiled{
		newMinutes: r.NewMinutes,
		words:      entries(r.Words, func(s string) string { return s }),
		phrases:    entries(r.Replacements, func(p Replacement) string { return p.Phrase }),
	}
	for _, p := range r.Replacements {
		c.with = append(c.with, slices.Clone(p.With))
	}
	for _, d := range r.Domains {
		if ascii, ok := domain(d); ok {
			c.domains = append(c.domains, ascii)
		}
	}
	return c
}

// Check says why a message is refused (nil: it is not). last: the author's
// last message (zero: none), for the rate of limited authors.
func Check(r Rules, a Author, m Message, now, last time.Time) error {
	return Compile(r).Check(a, m, now, last)
}

// Check is Check with the compiled rules.
func (c *Compiled) Check(a Author, m Message, now, last time.Time) error {
	if a.Staff {
		return nil
	}
	if blockedWord(c.words, m.Text) {
		return ErrWord
	}
	if blockedDomain(c.domains, m.Text) {
		return ErrDomain
	}
	r := Rules{NewMinutes: c.newMinutes}
	if !Limited(r, a, now) {
		return nil
	}
	refuse := func(what string) error {
		if a.Guest {
			return fmt.Errorf("Guests cannot %s: log in or make an account", what)
		}
		return fmt.Errorf("New accounts can %s after %d minutes", what, r.NewMinutes)
	}
	switch {
	case m.Upload:
		return refuse("upload files")
	case linkPattern.MatchString(clean(m.Text)):
		return refuse("post links")
	case m.MentionAll:
		return refuse("mention @everyone or @here")
	case m.Post && now.Sub(last) < Interval:
		who := "New accounts"
		if a.Guest {
			who = "Guests"
		}
		return fmt.Errorf("%s can send one message every %d seconds", who, int(Interval/time.Second))
	}
	return nil
}

// Normalize checks rules from an admin: entries trimmed, without
// duplicates (as matched), sorted; domains without scheme or path. Lists
// are never nil (JSON lists).
func Normalize(r Rules) (Rules, error) {
	if r.NewMinutes < 0 || r.NewMinutes > MaxNewMinutes {
		return Rules{}, fmt.Errorf("new accounts: 0 to %d minutes", MaxNewMinutes)
	}
	if len(r.Words) > maxEntries || len(r.Domains) > maxEntries || len(r.Replacements) > maxEntries {
		return Rules{}, fmt.Errorf("at most %d words, domains and replaced phrases", maxEntries)
	}
	out := Rules{NewMinutes: r.NewMinutes, Words: []string{}, Domains: []string{}, Replacements: []Replacement{}}
	seen := map[string]bool{}
	// phrase: an entry tidied, "" for none or a duplicate.
	phrase := func(w, what string) (string, error) {
		w = strings.Join(strings.Fields(w), " ")
		k := what + "\x00" + key(w)
		switch {
		case w == "" || seen[k]:
			return "", nil
		case utf8.RuneCountInString(w) > maxEntry:
			return "", fmt.Errorf("a %s has at most %d characters", what, maxEntry)
		case !wordEntry.MatchString(w):
			return "", fmt.Errorf("%q: use words of letters and digits (with ' - . _ between them)", w)
		}
		seen[k] = true
		return w, nil
	}
	for _, w := range r.Words {
		w, err := phrase(w, "blocked word")
		if err != nil {
			return Rules{}, err
		}
		if w != "" {
			out.Words = append(out.Words, w)
		}
	}
	for _, p := range r.Replacements {
		if strings.TrimSpace(p.Phrase) == "" {
			return Rules{}, errors.New("a replacement needs a phrase")
		}
		w, err := phrase(p.Phrase, "replaced phrase")
		if err != nil {
			return Rules{}, err
		} else if w == "" {
			continue // twice
		}
		with := []string{}
		for _, s := range p.With {
			s = strings.Join(strings.Fields(s), " ")
			switch {
			case s == "" || slices.Contains(with, s):
				continue
			case utf8.RuneCountInString(s) > maxEntry || !plainText.MatchString(s):
				return Rules{}, fmt.Errorf("%q: replacements are up to %d letters, digits, emoji, spaces and ' . , ! ? -", s, maxEntry)
			}
			with = append(with, s)
		}
		if len(with) == 0 || len(with) > maxWith {
			return Rules{}, fmt.Errorf("%q: 1 to %d replacements", w, maxWith)
		}
		out.Replacements = append(out.Replacements, Replacement{w, with})
	}
	for _, d := range r.Domains {
		if strings.TrimSpace(d) == "" {
			continue
		}
		ascii, ok := domain(d)
		if !ok {
			return Rules{}, fmt.Errorf("%q is not a domain", strings.TrimSpace(d))
		}
		if !seen["\x00"+ascii] {
			seen["\x00"+ascii] = true
			show, _ := idna.ToUnicode(ascii)
			out.Domains = append(out.Domains, show)
		}
	}
	slices.SortFunc(out.Words, func(a, b string) int { return strings.Compare(key(a), key(b)) })
	slices.SortFunc(out.Replacements, func(a, b Replacement) int { return strings.Compare(key(a.Phrase), key(b.Phrase)) })
	slices.Sort(out.Domains)
	return out, nil
}

// clean takes out what hides text without changing it: invisible format
// characters (zero-width spaces) and fillers (Hangul fillers, the Arabic
// tatweel), compatibility forms (full-width letters), ideographic full
// stops (dots in domains, as for browsers).
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) || strings.ContainsRune(fillers, r) {
			return -1
		}
		return r
	}, s)
	return strings.ReplaceAll(norm.NFKC.String(s), "。", ".")
}

// blockedWord: the text has a blocked word or phrase.
func blockedWord(blocked []entry, text string) bool {
	return len(blocked) > 0 && text != "" && len(find(blocked, text, false)) > 0
}

func entries[T any](list []T, phrase func(T) string) []entry {
	out := make([]entry, len(list))
	for i, x := range list {
		out[i] = entryOf(phrase(x))
	}
	return out
}

// Replace gives the text with the replaced phrases, each match with one of
// its replacements (pick(n): a random number below n), in the case of the
// match where simple: "Nice ass" → "Beautiful behind", "ASS" → "BEHIND".
// Not in code, links, mentions, channel names and emoji names, and only
// with separators (spaces . - _) between the words of a phrase: the
// message's markup stays. For everyone, staff too.
func Replace(r Rules, text string, pick func(int) int) string {
	return Compile(r).Replace(text, pick)
}

// Replace is Replace with the compiled rules.
func (c *Compiled) Replace(text string, pick func(int) int) string {
	if len(c.phrases) == 0 || text == "" {
		return text
	}
	keep := kept.FindAllStringIndex(text, -1)
	var b strings.Builder
	last := 0
	for _, m := range find(c.phrases, text, true) {
		if slices.ContainsFunc(keep, func(k []int) bool { return m.start < k[1] && k[0] < m.end }) {
			continue
		}
		with := c.with[m.entry]
		b.WriteString(text[last:m.start])
		b.WriteString(sameCase(text[m.start:m.end], with[pick(len(with))]))
		last = m.end
	}
	return b.String() + text[last:]
}

// sameCase: s in capitals if orig is (two letters or more), or with a
// capital first letter if orig has one.
func sameCase(orig, s string) string {
	var letters []rune
	for _, r := range orig {
		if unicode.IsLetter(r) {
			letters = append(letters, r)
		}
	}
	switch {
	case len(letters) == 0 || !unicode.IsUpper(letters[0]):
		return s
	case len(letters) > 1 && !slices.ContainsFunc(letters, func(r rune) bool { return !unicode.IsUpper(r) }):
		return strings.ToUpper(s)
	}
	first, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(first)) + s[n:]
}

const fillers = "\u0640\u115f\u1160\u3164\uffa0"

var (
	// Blocked words: words of letters, digits and marks, with spaces or
	// one of ' - . _ between them (other symbols would split a word, so
	// "c++" would block every "c").
	wordEntry = regexp.MustCompile(`^[\p{L}\p{N}\p{M}]+(?:(?: |['’._-])[\p{L}\p{N}\p{M}]+)*$`)
	// Where nothing is replaced: code, links, mentions, channels, emoji
	// (personal ones are "<:name:id>").
	kept = regexp.MustCompile("```[\\s\\S]*?```|`[^`\\n]+`|<:[a-z0-9_]+:[0-9]+>|[a-zA-Z][a-zA-Z0-9+.-]*://\\S+|www\\.\\S+|[@#][\\p{L}\\p{N}_-]+|:[\\p{L}\\p{N}_-]+:")
	// Replacements (as PLAIN in apps/web src/lib/messageRules.ts): words of letters, digits, marks and emoji with spaces
	// and ' . , ! ? - between them. Nothing that formats (* _ ~ ` | >),
	// links (:/), mentions (@ #) or is HTML.
	plainText = regexp.MustCompile(`^[\p{L}\p{N}\p{M}\p{So}\x{1F3FB}-\x{1F3FF}\x{200D}\x{FE0F}]+(?:[ '’.,!?-]+[\p{L}\p{N}\p{M}\p{So}\x{1F3FB}-\x{1F3FF}\x{200D}\x{FE0F}]+)*[.,!?]*$`)
	// Percent-encoded bytes (browsers decode them in hosts).
	percent = regexp.MustCompile(`%[0-9a-fA-F]{2}`)
	// Hosts in text: labels with dots between them.
	hostPattern = regexp.MustCompile(`[\p{L}\p{N}\p{M}-]+(?:\.[\p{L}\p{N}\p{M}-]+)+`)
	asciiHost   = regexp.MustCompile(`^[a-z0-9-]+(?:\.[a-z0-9-]+)*$`)
	// What clients make links of: a scheme, or www.
	linkPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://|(?:^|[^\p{L}\p{N}.-])www\.`)
)

// domain is a host (or URL) in its ASCII form, lower case.
func domain(s string) (string, bool) {
	s = strings.TrimSpace(clean(s))
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	}
	s, _, _ = strings.Cut(s, "/")
	s, _, _ = strings.Cut(s, "?")
	s, _, _ = strings.Cut(s, "#")
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	s, _, _ = strings.Cut(s, ":")
	s = strings.Trim(strings.TrimPrefix(s, "*."), ".")
	ascii, err := idna.Lookup.ToASCII(s)
	if err != nil {
		ascii = strings.ToLower(s)
	}
	return ascii, asciiHost.MatchString(ascii)
}

// blockedDomain: a host in the text is a blocked domain or below one
// (also percent-encoded, or not valid: browsers may still open it).
// blocked: in their ASCII form (see domain).
func blockedDomain(blocked []string, text string) bool {
	if len(blocked) == 0 || text == "" {
		return false
	}
	decoded := percent.ReplaceAllStringFunc(text, func(p string) string {
		b, _ := strconv.ParseUint(p[1:], 16, 8)
		return string([]byte{byte(b)}) // bytes in a row make UTF-8 again
	})
	for _, h := range hostPattern.FindAllString(clean(text+" "+decoded), -1) {
		host, _ := domain(h)
		for _, d := range blocked {
			if host == d || strings.HasSuffix(host, "."+d) {
				return true
			}
		}
	}
	return false
}
