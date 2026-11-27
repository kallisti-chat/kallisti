package msgrules

import (
	"fmt"
	"testing"
	"time"
)

// benchRules: n blocked words, domains and replaced phrases, as admins
// save them.
func benchRules(tb testing.TB, n int) Rules {
	var r Rules
	for i := range n {
		w := fmt.Sprintf("badword%d", i)
		if i%2 == 1 {
			w = fmt.Sprintf("bad phrase%d", i) // phrases too
		}
		r.Words = append(r.Words, w)
		r.Domains = append(r.Domains, fmt.Sprintf("spam%d.example", i))
		r.Replacements = append(r.Replacements, Replacement{Phrase: fmt.Sprintf("silly thing%d", i), With: []string{fmt.Sprintf("nice thing%d", i)}})
	}
	r, err := Normalize(r)
	if err != nil {
		tb.Fatal(err)
	}
	return r
}

const (
	benchText  = "Hey everyone, did you see the match last night? I thought the second half was great, and the keeper made some silly mistakes. Anyway, see you all on Friday for the game night!"
	benchLinks = "Hey everyone, see https://www.news.example.org/sport/match and https://bücher.example/a?b=c, or www.video.example/watch. Thoughts? Friday works for me."
)

// BenchmarkSend is a web message as the server checks it: the words
// replaced, the text checked, then the post checked again without text.
func BenchmarkSend(b *testing.B) {
	for _, n := range []int{50, 500} {
		r := benchRules(b, n)
		for _, c := range []struct{ name, text string }{{"text", benchText}, {"links", benchLinks}} {
			b.Run(fmt.Sprintf("entries=%d/%s", n, c.name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					text := Replace(r, c.text, func(int) int { return 0 })
					if err := Check(r, Author{}, Message{Text: text, Post: true}, now, time.Time{}); err != nil {
						b.Fatal(err)
					}
					if err := Check(r, Author{}, Message{Post: true}, now, time.Time{}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkCheckEmpty is the second check of a web post: no text.
func BenchmarkCheckEmpty(b *testing.B) {
	for _, n := range []int{50, 500} {
		r := benchRules(b, n)
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := Check(r, Author{}, Message{Post: true}, now, time.Time{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
