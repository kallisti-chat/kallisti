// Command seedchat fills a server's database with realistic text channels
// for manual tests (development only): a category "Community" with five
// channels of 200 to 350 messages over the last weeks, from about twenty
// people, with replies, mentions, reactions, pictures (one or several),
// polls with votes, threads and pins. Run it with the server stopped; the
// server makes the pictures' previews at its next start.
//
//	seedchat -db kallisti.db -uploads uploads -images <dir of JPEGs/GIFs>
//
// The authors have Mumble user ids from 900001 on, so no seeded message
// belongs to a real account. Channels that exist already are left alone.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"io"
	"log"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kallisti.chat/server/internal/store"
)

type person struct {
	name string
	id   uint32 // 0: a guest
}

var people = func() []person {
	names := []string{"mira", "jonas", "kaia", "Nerthos", "sam_k", "elif", "tobi", "rook", "yuki", "darius",
		"pixelfox", "hanna", "ollie", "Brenna", "vex", "matteo", "linnea", "quill", "ash@social.example", "juno@mastodon.example"}
	out := make([]person, 0, len(names)+2)
	for i, n := range names {
		out = append(out, person{n, uint32(900001 + i)})
	}
	return append(out, person{"guest_otter", 0}, person{"wanderer", 0})
}()

type channelSpec struct {
	name     string
	messages int
	pictures float64 // share of messages with pictures
	lines    []string
	polls    []store.Poll
	threads  []string
}

var common = []string{
	"lol", "same", "+1", "agreed", "true", "wait what", "oh nice", "that's wild", "haha yes", "fair point",
	"I'll check later", "brb", "back", "good morning everyone", "night all", "thanks!", "no worries",
	"honestly same", "this is the way", "can confirm", "yeah that makes sense", "hmm not sure about that",
	"oh no", "omg", "ok that is actually really good", "nice one", "did anyone else see that?", "🙃",
	"I was about to say the same thing", "ha, classic", "100%", "big if true", "wow", "love this",
}

var channels = []channelSpec{
	{name: "off-topic", messages: 340, pictures: 0.08, lines: []string{
		"anyone else completely forgot it was a holiday today",
		"my cat just knocked a full glass of water onto my keyboard. it still works somehow",
		"what are you all having for dinner? I need ideas",
		"the weather here went from summer to winter in about two hours",
		"finally finished moving. never again, I'm living in this flat forever",
		"is it just me or are trains always late on mondays",
		"made bread for the first time, it is very flat but tastes great",
		"hot take: pineapple on pizza is fine and we should all move on",
		"just discovered you can mute group chats on your phone per hour. life changing",
		"my neighbour started drilling at 7am again",
		"what's everyone's weekend plan?",
		"went hiking yesterday and my legs are still not talking to me",
		"does anyone know a good repair shop for headphones? the left side stopped working",
		"I think I drank too much coffee, I can hear colours",
		"today I learned that octopuses have three hearts",
		"the new season of that cooking show is out and I already watched half of it",
		"someone left a whole box of free books on the street, took four",
		"can we talk about how expensive groceries got",
		"I need a new hobby that is not buying more plants",
		"the sunset tonight was unreal, wish I had a better camera",
		"my plants are thriving and I don't know what I'm doing right",
		"just booked flights for the spring, so excited",
		"anyone here into board games? we are looking for a fourth player on thursdays",
		"who else is still awake",
		"tried that new ramen place downtown, 9/10 would slurp again",
		"why do all printers hate me personally",
		"spent the whole afternoon cleaning and now I can't find anything",
		"my sister got a puppy and I am now visiting her every day for some reason",
	}, polls: []store.Poll{
		{Question: "Best time for the next community meetup?", Answers: []store.PollAnswer{{Text: "Friday evening"}, {Text: "Saturday afternoon"}, {Text: "Sunday brunch"}}},
		{Question: "Tea or coffee?", Answers: []store.PollAnswer{{Text: "Tea", Emoji: "🍵"}, {Text: "Coffee", Emoji: "☕"}, {Text: "Both, at the same time"}, {Text: "Neither"}}},
	}, threads: []string{"Meetup planning", "Recipe swap"}},
	{name: "gaming", messages: 300, pictures: 0.12, lines: []string{
		"anyone up for a few rounds tonight?",
		"I finally beat that boss after like 40 tries",
		"the new patch completely broke my build lol",
		"is the dlc worth it? reviews seem mixed",
		"we need one more for the raid at 9",
		"just hit diamond rank, my hands are shaking",
		"the soundtrack of this game is so good",
		"steam sale is going to ruin me again",
		"who wants to try the co-op mode this weekend",
		"servers are down again, classic",
		"I've put 200 hours into this and I still don't understand the crafting",
		"their speedrun record just dropped by 3 seconds, insane",
		"recommend me a cozy game for a rainy sunday",
		"that ending had me sitting in silence for a while",
		"my controller drift is getting worse, time for a new one",
		"queue times are terrible right now",
		"tried the demo, the movement feels great",
		"anyone else playing the beta? need invite codes?",
		"I'm streaming in an hour if anyone wants to hang out",
		"lag killed me three times in a row",
		"the level design in the third act is incredible",
		"remember LAN parties? we should do one",
		"just got the rarest drop in the game on my first try",
		"patch notes are up, they nerfed my main",
	}, polls: []store.Poll{
		{Question: "What should we play on game night?", Answers: []store.PollAnswer{{Text: "Co-op shooter"}, {Text: "Racing"}, {Text: "Party games"}, {Text: "Something new"}}, Multiple: true},
	}, threads: []string{"Raid on Friday", "Build advice"}},
	{name: "music", messages: 220, pictures: 0.1, lines: []string{
		"this album has been on repeat all week",
		"anyone going to the concert next month?",
		"just found an old mixtape from 2009, it is a time capsule",
		"what are you listening to right now?",
		"new single dropped and it's so different from their old stuff",
		"I've been learning guitar for three months, my fingers hurt",
		"vinyl just sounds warmer, fight me",
		"recommend me something like this but slower",
		"the live version is so much better than the studio one",
		"jazz at 2am hits different",
		"my spotify wrapped is going to be embarrassing this year",
		"anyone know the name of the song from the trailer?",
		"saw them live last night, my ears are still ringing",
		"bought a cheap synth and lost the whole weekend to it",
		"playlist for focus work, anyone?",
		"that bassline is unreal",
		"the drummer in this band is criminally underrated",
		"covers that are better than the original, go",
	}, polls: []store.Poll{
		{Question: "Album of the year so far?", Answers: []store.PollAnswer{{Text: "The indie one"}, {Text: "The comeback album"}, {Text: "The debut"}}, Anonymous: true},
	}, threads: []string{"Shared playlist"}},
	{name: "photos", messages: 230, pictures: 0.55, lines: []string{
		"view from my window this morning",
		"took this on my walk today",
		"golden hour never disappoints",
		"new lens, still figuring it out",
		"found this little spot in the forest",
		"film photos from the summer finally developed",
		"rate my setup",
		"the light was perfect for about five minutes",
		"edited this one a bit, too much?",
		"some shots from the weekend trip",
		"tried long exposure for the first time",
		"city at night",
		"phone camera, no edits",
		"which one do you like more?",
		"my favourite shot from the year",
	}, polls: []store.Poll{
		{Question: "Theme for next week's photo challenge?", Answers: []store.PollAnswer{{Text: "Reflections"}, {Text: "Shadows"}, {Text: "Something red"}, {Text: "Street"}}},
	}, threads: []string{"Photo challenge: water", "Gear talk"}},
	{name: "dev-talk", messages: 260, pictures: 0.05, lines: []string{
		"anyone else fighting with dependency versions today?",
		"finally got the CI green after two days",
		"is it worth migrating to the new framework version?",
		"rubber duck debugging works way too well",
		"the bug was a missing semicolon. of course it was",
		"what editor theme are you all using?",
		"I wrote a script to automate my morning setup and now it takes longer",
		"code review tip: be kind, everyone was a junior once",
		"just deleted 2000 lines of code and everything still works",
		"tabs or spaces? I'm not starting anything, just asking",
		"the docs say it's supported, the docs lie",
		"pair programming session later if anyone wants to join",
		"how do you all handle secrets in local dev?",
		"spent all morning on a timezone bug. never again",
		"TIL you can `git commit --fixup` and then autosquash",
		"```\nfor (const m of messages) {\n  if (!m.seen) notify(m);\n}\n```\nwhy does this notify twice?",
		"have a look: https://example.com/blog/structured-logging",
		"`SELECT count(*) FROM messages WHERE channel_id = 1` is slow on the big table, any ideas?",
		"the new release fixed the memory leak we were seeing",
		"writing tests first actually saved me today",
		"anyone tried the new language server? autocomplete is way faster",
		"production is fine. staging is on fire. normal tuesday",
	}, polls: []store.Poll{
		{Question: "Which language should we use for the side project?", Answers: []store.PollAnswer{{Text: "Go"}, {Text: "Rust"}, {Text: "TypeScript"}, {Text: "Elixir"}}, Multiple: true},
	}, threads: []string{"Side project kickoff", "CI is flaky again"}},
}

var threadLines = []string{
	"ok so here's what I'm thinking", "I'm in", "count me in too", "what time works for everyone?",
	"I can do anything after 6", "let's write down a list", "good idea", "I'll bring snacks",
	"can someone summarize for people who just joined?", "updated the plan above", "sounds good to me",
	"one more question about this", "done, see the link", "nice, thanks for organizing", "👍",
	"I might be a bit late", "same here", "let's decide by tomorrow", "posting the details soon",
}

var emojis = []string{"👍", "😂", "❤️", "🔥", "👀", "🎉", "😮", "💯", "🙏", "😅"}

func main() {
	db := flag.String("db", "kallisti.db", "the database")
	uploads := flag.String("uploads", "uploads", "the upload directory")
	images := flag.String("images", "", "a directory of JPEG and GIF pictures to attach")
	days := flag.Int("days", 30, "days of history")
	seed := flag.Uint64("seed", 1, "random seed")
	flag.Parse()
	s, err := store.Open(*db)
	if err != nil {
		log.Fatal(err)
	}
	pics, err := pictures(*images)
	if err != nil {
		log.Fatal(err)
	}
	g := &seeder{s: s, r: mrand.New(mrand.NewPCG(*seed, 7)), uploads: *uploads, pics: pics, now: time.Now()}
	g.start = g.now.Add(-time.Duration(*days) * 24 * time.Hour)
	category, err := g.category("Community")
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range channels {
		ch, err := s.CreateChannel(c.name, category, false)
		if errors.Is(err, store.ErrNameTaken) {
			log.Printf("#%s exists: left alone", c.name)
			continue
		} else if err != nil {
			log.Fatal(err)
		}
		n, err := g.fill(ch.ID, c)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("#%s: %d messages", c.name, n)
	}
}

type seeder struct {
	s          *store.Store
	r          *mrand.Rand
	uploads    string
	pics       []picture
	start, now time.Time
}

type picture struct {
	path, typ     string
	width, height int
}

// pictures are the JPEGs and GIFs in dir, with their sizes.
func pictures(dir string) ([]picture, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []picture
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		typ := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif"}[ext]
		if typ == "" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, picture{p, typ, cfg.Width, cfg.Height})
	}
	return out, nil
}

func (g *seeder) category(name string) (int64, error) {
	cats, err := g.s.Categories()
	if err != nil {
		return 0, err
	}
	for _, c := range cats {
		if c.Name == name {
			return c.ID, nil
		}
	}
	c, err := g.s.CreateCategory(name)
	return c.ID, err
}

// times are n moments between start and now, more in the evening, few at
// night, in bursts (conversations).
func (g *seeder) times(n int, from, to time.Time) []time.Time {
	out := make([]time.Time, 0, n)
	span := to.Sub(from)
	for len(out) < n {
		t := from.Add(time.Duration(g.r.Int64N(int64(span))))
		h := t.Hour()
		if (h < 8 && g.r.Float64() < 0.85) || (h >= 8 && h < 17 && g.r.Float64() < 0.35) {
			continue
		}
		// A burst: a few messages within minutes.
		for k := 1 + g.r.IntN(5); k > 0 && len(out) < n; k-- {
			t = t.Add(time.Duration(10+g.r.IntN(180)) * time.Second)
			if t.Before(to) {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

func (g *seeder) pick(list []string) string { return list[g.r.IntN(len(list))] }

func (g *seeder) person(last *person) person {
	if last != nil && g.r.Float64() < 0.35 {
		return *last // the same person again: messages group
	}
	return people[g.r.IntN(len(people))]
}

func (g *seeder) text(lines []string) string {
	t := g.pick(lines)
	if g.r.Float64() < 0.4 {
		t = g.pick(common)
	}
	if g.r.Float64() < 0.06 {
		t = "@" + people[g.r.IntN(len(people)-2)].name + " " + t
	}
	if g.r.Float64() < 0.05 {
		t += "\n\n" + g.pick(lines)
	}
	return t
}

// attach copies a picture into the uploads under a new key and stores it.
func (g *seeder) attach(author string, at time.Time) (store.Attachment, error) {
	p := g.pics[g.r.IntN(len(g.pics))]
	b := make([]byte, 16)
	rand.Read(b)
	key := hex.EncodeToString(b)
	size, err := copyFile(p.path, filepath.Join(g.uploads, key))
	if err != nil {
		return store.Attachment{}, err
	}
	name := fmt.Sprintf("IMG_%04d%s", 1000+g.r.IntN(9000), filepath.Ext(p.path))
	u, err := g.s.AddUpload(store.Upload{Key: key, Name: name, Type: p.typ, Size: size, Author: author, Media: store.Media{Width: p.width, Height: p.height}}, at)
	return store.Attachment{ID: u.ID}, err
}

func copyFile(from, to string) (int64, error) {
	in, err := os.Open(from)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

func (g *seeder) message(channel int64, thread bool, p person, text string, at time.Time, reply int64, pictureShare float64, poll *store.Poll) (store.Message, error) {
	n := store.NewMessage{ChannelID: channel, Thread: thread, Author: p.name, Verified: p.id != 0, AuthorID: p.id, Text: text, At: at, ReplyTo: reply, Poll: poll}
	if poll == nil && len(g.pics) > 0 && g.r.Float64() < pictureShare {
		count := 1
		if g.r.Float64() < 0.3 {
			count = 2 + g.r.IntN(3)
		}
		for range count {
			a, err := g.attach(p.name, at)
			if err != nil {
				return store.Message{}, err
			}
			n.Uploads = append(n.Uploads, a)
		}
		if g.r.Float64() < 0.4 {
			n.Text = "" // pictures only
		}
	}
	m, err := g.s.AddMessage(n)
	if err != nil {
		return m, err
	}
	return m, g.react(m.ID, at, 0.25)
}

// react gives a message reactions from registered people (share: how many
// messages get any).
func (g *seeder) react(id int64, at time.Time, share float64) error {
	if g.r.Float64() >= share {
		return nil
	}
	for e := 1 + g.r.IntN(3); e > 0; e-- {
		emoji := g.pick(emojis)
		for k := 1 + g.r.IntN(6); k > 0; k-- {
			p := people[g.r.IntN(len(people)-2)]
			if _, err := g.s.SetReaction(id, emoji, p.id, p.name, true, at.Add(time.Minute)); err != nil {
				return err
			}
		}
	}
	return nil
}

// fill writes a channel's messages, polls, threads and pins; the count.
func (g *seeder) fill(channel int64, c channelSpec) (int, error) {
	times := g.times(c.messages, g.start, g.now.Add(-10*time.Minute))
	pollAt := map[int]int{}
	for i := range c.polls {
		pollAt[(i+1)*len(times)/(len(c.polls)+1)] = i
	}
	var ids []int64
	var last *person
	threads := map[int]string{}
	for i, name := range c.threads {
		threads[(2*i+1)*len(times)/(2*len(c.threads)+1)+3] = name
	}
	for i, at := range times {
		p := g.person(last)
		last = &p
		var reply int64
		if len(ids) > 3 && g.r.Float64() < 0.1 {
			reply = ids[len(ids)-1-g.r.IntN(min(12, len(ids)))]
		}
		var poll *store.Poll
		text := g.text(c.lines)
		if k, ok := pollAt[i]; ok {
			pl := c.polls[k]
			pl.EndsAt = at.Add(7 * 24 * time.Hour)
			poll, text, reply = &pl, "", 0
		}
		m, err := g.message(channel, false, p, text, at, reply, c.pictures, poll)
		if err != nil {
			return 0, fmt.Errorf("message %d: %w", i, err)
		}
		ids = append(ids, m.ID)
		if poll != nil {
			if err := g.votes(m.ID, *poll, at); err != nil {
				return 0, err
			}
		}
		if name, ok := threads[i]; ok {
			if err := g.thread(channel, m.ID, name, p, at); err != nil {
				return 0, err
			}
		}
	}
	for k := 0; k < 3 && len(ids) > 10; k++ {
		if _, _, err := g.s.SetPinned(ids[g.r.IntN(len(ids)-5)], g.now); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func (g *seeder) votes(id int64, p store.Poll, at time.Time) error {
	for _, v := range people[:len(people)-2] {
		if g.r.Float64() < 0.35 {
			continue
		}
		answers := []int{g.r.IntN(len(p.Answers))}
		if p.Multiple && g.r.Float64() < 0.4 {
			if b := g.r.IntN(len(p.Answers)); b != answers[0] {
				answers = append(answers, b)
			}
		}
		at = at.Add(time.Duration(1+g.r.IntN(120)) * time.Minute)
		if _, err := g.s.Vote(id, store.Voter{ID: v.id, Name: v.name}, answers, at); err != nil {
			return fmt.Errorf("vote: %w", err)
		}
	}
	return nil
}

// thread starts a thread from a message, with 10 to 40 messages over the
// next hours or days.
func (g *seeder) thread(parent, start int64, name string, p person, at time.Time) error {
	var id *uint32
	if p.id != 0 {
		id = &p.id
	}
	t, err := g.s.CreateThread(parent, start, name, p.name, id, at.Add(time.Minute))
	if err != nil {
		return fmt.Errorf("thread: %w", err)
	}
	end := at.Add(time.Duration(6+g.r.IntN(72)) * time.Hour)
	if end.After(g.now) {
		end = g.now.Add(-time.Minute)
	}
	if end.Sub(at) < time.Hour {
		return nil // too close to now for a conversation
	}
	var last *person
	for _, mt := range g.times(10+g.r.IntN(31), at.Add(2*time.Minute), end) {
		q := g.person(last)
		last = &q
		if _, err := g.message(t.ID, true, q, g.pick(threadLines), mt, 0, 0.05, nil); err != nil {
			return fmt.Errorf("thread message: %w", err)
		}
	}
	return nil
}
