package msgrules

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The matching as it was before the rules were compiled, each entry
// folded or converted for each message: compiled rules match the same.

func oldBlockedWord(blocked []string, text string) bool {
	return len(blocked) > 0 && len(find(entries(blocked, func(s string) string { return s }), text, false)) > 0
}

func oldBlockedDomain(blocked []string, text string) bool {
	if len(blocked) == 0 {
		return false
	}
	decoded := percent.ReplaceAllStringFunc(text, func(p string) string {
		b, _ := strconv.ParseUint(p[1:], 16, 8)
		return string([]byte{byte(b)})
	})
	for _, h := range hostPattern.FindAllString(clean(text+" "+decoded), -1) {
		host, _ := domain(h)
		for _, b := range blocked {
			if d, ok := domain(b); ok && (host == d || strings.HasSuffix(host, "."+d)) {
				return true
			}
		}
	}
	return false
}

func oldCheck(r Rules, a Author, m Message, now, last time.Time) error {
	if a.Staff {
		return nil
	}
	if oldBlockedWord(r.Words, m.Text) {
		return ErrWord
	}
	if oldBlockedDomain(r.Domains, m.Text) {
		return ErrDomain
	}
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

func oldReplace(r Rules, text string, pick func(int) int) string {
	if len(r.Replacements) == 0 {
		return text
	}
	keep := kept.FindAllStringIndex(text, -1)
	var b strings.Builder
	last := 0
	for _, m := range find(entries(r.Replacements, func(p Replacement) string { return p.Phrase }), text, true) {
		if slices.ContainsFunc(keep, func(k []int) bool { return m.start < k[1] && k[0] < m.end }) {
			continue
		}
		with := r.Replacements[m.entry].With
		b.WriteString(text[last:m.start])
		b.WriteString(sameCase(text[m.start:m.end], with[pick(len(with))]))
		last = m.end
	}
	return b.String() + text[last:]
}

func TestCompiledMatchesTheSame(t *testing.T) {
	full := benchRules(t, maxEntries)
	full.Words[len(full.Words)-1] = "spam" // the last entry at the cap
	full.Domains[len(full.Domains)-1] = "bücher.example"
	rules := []Rules{
		{},
		{Words: []string{}, Domains: []string{}, Replacements: []Replacement{}},
		{Words: []string{"spam", "buy now", "Straße", "café", "広告", "нах", "bot", "ass", "nice ass"}},
		// As saved, and as an admin may have typed them before Normalize
		// (entries that are not domains are left out).
		{Domains: []string{"spam.example", "bücher.example", "xyz", "xn--bcher-kva.example", "*.wild.example"}},
		{Domains: []string{"https://Spam.Example/path?q", "not a domain", "", "  ", "spam.example."}},
		{Replacements: []Replacement{
			{Phrase: "nice ass", With: []string{"beautiful behind", "lovely posterior"}},
			{Phrase: "ass", With: []string{"behind"}},
			{Phrase: "広告", With: []string{"宣伝"}},
			{Phrase: "12", With: []string{"twelve"}},
		}},
		{NewMinutes: 10, Words: []string{"spam"}, Domains: []string{"spam.example"}, Replacements: []Replacement{{Phrase: "spam", With: []string{"ham"}}}},
		full,
	}
	texts := []string{
		"", " ", "hello", "this is spam", "SPAM!", "spammer", "s p a m", "s.p.a.m!", "ѕраm", "ЅΡΑМ", "𝐬𝐩𝐚𝐦", "ｓｐａｍ",
		"s​pam", "spㅤam", "spaaam", "spaam", "STRASSE", "CAFÉ", "café", "これは広告です", "НАХ", "ВОТ",
		"buy   now", "buy\nnow", "what a nice ass", "Nice ass!", "NICE ASS", "a.s.s.", "аss", "kick-ass", "||nice ass||",
		"`nice ass` ```\nass\n``` :nice_ass: @ass #ass", "<:nice_ass:12> 12", "**nice** ass", "nice - ass",
		"see https://spam.example/x", "spam.example", "SPAM.EXAMPLE", "https://www.spam.example", "https://a.b.spam.example:8080/",
		"notspam.example", "https://spam.example.org", "[click](https://spam.example/x)", "https://good.example@spam.example/",
		"spam。example", "spam​.example", "ｓｐａｍ．ｅｘａｍｐｌｅ", "https://BÜCHER.example/", "https://xn--bcher-kva.example/",
		"https://buy.cheap.xyz", "https://spam%2Eexample/", "https://אa.spam.example/", "xyz", "spam example",
		"x.wild.example", "ok.example and ok.example and spam.example", "spam.example spam.example",
		"spam499.example", "www.spam250.example/x", "badword498 bad phrase499", "silly thing498 and Silly Thing2",
		benchText, benchLinks,
	}
	authors := []Author{{}, {Staff: true}, {Guest: true}, {Since: now.Add(-time.Minute)}}
	words, domains, replaced := 0, 0, 0 // the table has matches
	for i, r := range rules {
		c := Compile(r)
		for _, text := range texts {
			got, want := blockedWord(c.words, text), oldBlockedWord(r.Words, text)
			if got != want {
				t.Errorf("rules %d, %q: word %v, was %v", i, text, got, want)
			} else if got {
				words++
			}
			got, want = blockedDomain(c.domains, text), oldBlockedDomain(r.Domains, text)
			if got != want {
				t.Errorf("rules %d, %q: domain %v, was %v", i, text, got, want)
			} else if got {
				domains++
			}
			if got, want := c.Replace(text, picks(1, 0)), oldReplace(r, text, picks(1, 0)); got != want {
				t.Errorf("rules %d, %q: replaced %q, was %q", i, text, got, want)
			} else if got != text {
				replaced++
			}
			for _, a := range authors {
				for _, m := range []Message{{Text: text}, {Text: text, Post: true}, {Text: text, Upload: true}, {Text: text, MentionAll: true}} {
					got, want := fmt.Sprint(c.Check(a, m, now, now.Add(-time.Second))), fmt.Sprint(oldCheck(r, a, m, now, now.Add(-time.Second)))
					if got != want {
						t.Errorf("rules %d, %+v, %+v: %s, want %s", i, a, m, got, want)
					}
				}
			}
		}
	}
	if words < 20 || domains < 20 || replaced < 10 {
		t.Errorf("few matches: %d words, %d domains, %d replaced", words, domains, replaced)
	}
}

// Compiled rules keep what they were made from.
func TestCompiledIsACopy(t *testing.T) {
	r := Rules{Replacements: []Replacement{{Phrase: "ass", With: []string{"behind"}}}}
	c := Compile(r)
	r.Replacements[0].With[0] = "changed"
	if got := c.Replace("ass", picks(0)); got != "behind" {
		t.Errorf("%q", got)
	}
}
