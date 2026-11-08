package store

import (
	"errors"
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// threadStore has the text channels general (1) and links (2), and a
// message in general to start threads from.
func threadStore(t *testing.T) (*Store, Message) {
	t.Helper()
	s := open(t)
	if err := s.EnsureChannels([]string{"general", "links"}); err != nil {
		t.Fatal(err)
	}
	m, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", Text: "shall we play?", At: t0})
	if err != nil {
		t.Fatal(err)
	}
	return s, m
}

func addTo(t *testing.T, s *Store, channel int64, text string, at time.Time) Message {
	t.Helper()
	m, err := s.AddMessage(NewMessage{ChannelID: channel, Author: "bob", Text: text, At: at})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCreatingThreads(t *testing.T) {
	s, start := threadStore(t)
	creator := uint32(20)
	th, err := s.CreateThread(1, start.ID, "Games", "bob", &creator, t0)
	if err != nil || th.Parent != 1 || th.Start != start.ID || th.Name != "Games" || th.Author != "bob" || *th.AuthorID != 20 {
		t.Fatalf("create %+v %v", th, err)
	}
	if th.ID == 0 || th.ID == 1 || th.ID == 2 || th.Replies != 0 || !th.LastAt.Equal(t0) || th.Archived || th.StartAuthor != "alice" {
		t.Fatalf("new thread %+v", th)
	}
	if _, err := s.CreateThread(1, start.ID, "Again", "bob", nil, t0); !errors.Is(err, ErrThreadExists) {
		t.Fatalf("a second thread on a message: %v", err)
	}
	if _, err := s.CreateThread(2, start.ID, "Elsewhere", "bob", nil, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a message of another channel: %v", err)
	}
	// Not in a thread: one level.
	inside := addTo(t, s, th.ID, "yes", t0)
	if _, err := s.CreateThread(th.ID, inside.ID, "Deeper", "bob", nil, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a thread in a thread: %v", err)
	}
	// Its channel is no text channel: not listed, not renamed or hidden by
	// text channel changes.
	if cs, _ := s.Channels(); !slices.Equal(channelNames(cs), []string{"general", "links"}) {
		t.Fatalf("channels %v", channelNames(cs))
	}
	if err := s.RenameChannel(th.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename a thread as a channel: %v", err)
	}
	if err := s.EnsureChannels([]string{"general"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Thread(th.ID); err != nil {
		t.Fatalf("thread after EnsureChannels: %v", err)
	}
	if _, err := s.Thread(1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a text channel as a thread: %v", err)
	}
}

func TestThreadRepliesAndStartMessage(t *testing.T) {
	s, start := threadStore(t)
	th, _ := s.CreateThread(1, start.ID, "Games", "bob", nil, t0)
	addTo(t, s, th.ID, "one", t0.Add(time.Minute))
	addTo(t, s, th.ID, "two", t0.Add(2*time.Minute))
	got, err := s.Thread(th.ID)
	if err != nil || got.Replies != 2 || !got.LastAt.Equal(t0.Add(2*time.Minute)) || got.StartText != "shall we play?" {
		t.Fatalf("thread %+v %v", got, err)
	}
	// Messages carry the thread they started.
	hist, _ := s.History(1, 0, 10)
	if len(hist) != 1 || hist[0].Thread == nil || hist[0].Thread.ID != th.ID || hist[0].Thread.Replies != 2 {
		t.Fatalf("history %+v", hist)
	}
	if m, _ := s.MessageByID(start.ID); m.Thread == nil || m.Thread.Name != "Games" {
		t.Fatalf("message %+v", m)
	}
	// The start message goes, the thread stays (without its text).
	if _, err := s.DeleteMessage(start.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Thread(th.ID); err != nil || got.StartAuthor != "" || got.Replies != 2 {
		t.Fatalf("after the start message went %+v %v", got, err)
	}
	if got, err := s.ThreadOf(start.ID); err != nil || got.ID != th.ID {
		t.Fatalf("thread of a deleted message %+v %v", got, err)
	}
}

func TestRenamingArchivingAndListingThreads(t *testing.T) {
	s, start := threadStore(t)
	a, _ := s.CreateThread(1, start.ID, "A", "bob", nil, t0)
	second := addTo(t, s, 1, "another", t0)
	b, _ := s.CreateThread(1, second.ID, "B", "bob", nil, t0.Add(time.Hour))
	inLinks := addTo(t, s, 2, "a link", t0)
	c, _ := s.CreateThread(2, inLinks.ID, "C", "bob", nil, t0)
	if err := s.RenameThread(a.ID, "Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameThread(99, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename unknown: %v", err)
	}
	if changed, err := s.SetThreadArchived(a.ID, true); err != nil || !changed {
		t.Fatalf("archive %v %v", changed, err)
	}
	if changed, _ := s.SetThreadArchived(a.ID, true); changed {
		t.Fatal("archived twice")
	}
	names := func(ts []Thread) []string {
		out := []string{}
		for _, t := range ts {
			out = append(out, t.Name)
		}
		return out
	}
	if active, _ := s.Threads(0, false, 0, 0); !slices.Equal(names(active), []string{"C", "B"}) {
		t.Fatalf("active everywhere %v", names(active))
	}
	if active, _ := s.Threads(1, false, 0, 0); !slices.Equal(names(active), []string{"B"}) {
		t.Fatalf("active in general %v", names(active))
	}
	s.SetThreadArchived(b.ID, true)
	// Archived: newest first, paged by id.
	if page, _ := s.Threads(1, true, 0, 1); !slices.Equal(names(page), []string{"B"}) {
		t.Fatalf("first page %v", names(page))
	}
	if page, _ := s.Threads(1, true, b.ID, 1); !slices.Equal(names(page), []string{"Alpha"}) {
		t.Fatalf("second page %v", names(page))
	}
	if parents, _ := s.ThreadParents(); len(parents) != 3 || parents[c.ID] != 2 || parents[a.ID] != 1 {
		t.Fatalf("parents %v", parents)
	}
}

func TestArchivingIdleThreads(t *testing.T) {
	s, start := threadStore(t)
	quiet, _ := s.CreateThread(1, start.ID, "Quiet", "bob", nil, t0)
	second := addTo(t, s, 1, "another", t0)
	busy, _ := s.CreateThread(1, second.ID, "Busy", "bob", nil, t0)
	addTo(t, s, busy.ID, "still here", t0.Add(2*time.Hour))
	ids, err := s.ArchiveIdleThreads(t0.Add(time.Hour))
	if err != nil || !slices.Equal(ids, []int64{quiet.ID}) {
		t.Fatalf("archived %v %v", ids, err)
	}
	if got, _ := s.Thread(quiet.ID); !got.Archived {
		t.Fatal("not archived")
	}
	if ids, _ := s.ArchiveIdleThreads(t0.Add(time.Hour)); len(ids) != 0 {
		t.Fatalf("again %v", ids)
	}
}

func TestThreadArchiveSetting(t *testing.T) {
	s := open(t)
	if got, err := s.ThreadArchiveAfter(); err != nil || got != 3*24*time.Hour {
		t.Fatalf("default %v %v", got, err)
	}
	if err := s.SetThreadArchiveAfter(time.Hour); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ThreadArchiveAfter(); got != time.Hour {
		t.Fatalf("set %v", got)
	}
}

func TestFollowingThreads(t *testing.T) {
	s, start := threadStore(t)
	th, _ := s.CreateThread(1, start.ID, "Games", "bob", nil, t0)
	if changed, err := s.Follow(20, th.ID, true); err != nil || !changed {
		t.Fatalf("follow %v %v", changed, err)
	}
	if changed, _ := s.Follow(20, th.ID, true); changed {
		t.Fatal("followed twice")
	}
	s.Follow(30, th.ID, true)
	if f, _ := s.Followers(th.ID); !slices.Equal(f, []uint32{20, 30}) {
		t.Fatalf("followers %v", f)
	}
	if changed, _ := s.Follow(30, th.ID, false); !changed {
		t.Fatal("unfollow")
	}
	if f, _ := s.Followed(20); !slices.Equal(f, []int64{th.ID}) {
		t.Fatalf("followed %v", f)
	}
	if f, _ := s.Followed(30); len(f) != 0 {
		t.Fatalf("followed after unfollow %v", f)
	}
}

func TestDeletingThreads(t *testing.T) {
	s, start := threadStore(t)
	th, _ := s.CreateThread(1, start.ID, "Games", "bob", nil, t0)
	up, _ := s.AddUpload(Upload{Key: "k1", Name: "a.png", Type: "image/png", Size: 1, Author: "bob"}, t0)
	if _, err := s.AddMessage(NewMessage{ChannelID: th.ID, Author: "bob", Text: "pic", At: t0, Uploads: []Attachment{{ID: up.ID}}}); err != nil {
		t.Fatal(err)
	}
	reply := addTo(t, s, th.ID, "nice", t0)
	if _, err := s.SetReaction(reply.ID, "👍", 20, "bob", true, t0); err != nil {
		t.Fatal(err)
	}
	s.Follow(20, th.ID, true)
	s.SetReadMark(20, th.ID, reply.ID)
	keys, err := s.DeleteThread(th.ID)
	if err != nil || !slices.Equal(keys, []string{"k1"}) {
		t.Fatalf("delete %v %v", keys, err)
	}
	if _, err := s.Thread(th.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("thread after delete: %v", err)
	}
	if msgs, _ := s.History(th.ID, 0, 10); len(msgs) != 0 {
		t.Fatalf("messages after delete %v", msgs)
	}
	if f, _ := s.Followed(20); len(f) != 0 {
		t.Fatalf("follows after delete %v", f)
	}
	if marks, _ := s.ReadMarks(20); len(marks) != 0 {
		t.Fatalf("read marks after delete %v", marks)
	}
	if m, _ := s.MessageByID(start.ID); m.Thread != nil {
		t.Fatalf("start message keeps %+v", m.Thread)
	}
	// A new thread on the same message, with a new id (clients keep ids).
	if again, err := s.CreateThread(1, start.ID, "Again", "bob", nil, t0); err != nil || again.ID == th.ID {
		t.Fatalf("again %+v %v", again, err)
	}
	if _, err := s.DeleteThread(99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete unknown: %v", err)
	}
}

// A post to a thread deleted after it was checked is refused, and its
// upload stays unused (for the cleanup).
func TestPostingToADeletedThread(t *testing.T) {
	s, start := threadStore(t)
	th, _ := s.CreateThread(1, start.ID, "Games", "bob", nil, t0)
	if _, err := s.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	up, _ := s.AddUpload(Upload{Key: "k1", Name: "a.png", Type: "image/png", Size: 1, Author: "bob"}, t0)
	if _, err := s.AddMessage(NewMessage{ChannelID: th.ID, Thread: true, Author: "bob", Text: "pic", At: t0, Uploads: []Attachment{{ID: up.ID}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("post to a deleted thread: %v", err)
	}
	if msgs, _ := s.History(th.ID, 0, 10); len(msgs) != 0 {
		t.Fatalf("messages %v", msgs)
	}
	if keys, _ := s.DeleteUnusedUploads(t0.Add(time.Second)); !slices.Equal(keys, []string{"k1"}) {
		t.Fatalf("unused uploads %v", keys)
	}
}

func TestDeleteLargeThreadHistory(t *testing.T) {
	s, start := threadStore(t)
	th, err := s.CreateThread(1, start.ID, "Large", "bob", nil, t0)
	if err != nil {
		t.Fatal(err)
	}
	// One statement keeps setup fast without testing SQLite's bind limit.
	if _, err := s.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x < 32767)
		INSERT INTO messages(channel_id, author, text, created_at) SELECT ?, 'bob', 'history body', 0 FROM n`, th.ID); err != nil {
		t.Fatal(err)
	}
	var reply int64
	if err := s.db.QueryRow(`SELECT min(id) FROM messages WHERE channel_id = ?`, th.ID).Scan(&reply); err != nil {
		t.Fatal(err)
	}
	up, err := s.AddUpload(Upload{Key: "large-file", Name: "a.png", Type: "image/png", Size: 1, Author: "bob"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE uploads SET used = 1, message_id = ? WHERE key = 'large-file'`,
		`INSERT INTO polls(message_id, question, answers, ends_at) VALUES (?, 'question', '[]', 0)`,
		`INSERT INTO poll_votes(message_id, user_id, answer, user, created_at) VALUES (?, 20, 0, 'bob', 0)`,
		`INSERT INTO reactions(message_id, emoji, user_id, user, created_at) VALUES (?, 'ok', 20, 'bob', 0)`,
	} {
		if _, err := s.db.Exec(q, reply); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Follow(20, th.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReadMark(20, th.ID, reply); err != nil {
		t.Fatal(err)
	}
	keys, err := s.DeleteThread(th.ID)
	if err != nil || !slices.Equal(keys, []string{up.Key}) {
		t.Fatalf("delete: %v %v", keys, err)
	}
	for _, table := range []string{"uploads", "polls", "poll_votes", "reactions", "threads", "thread_follows", "read_marks_by_user"} {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d %v", table, n, err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("remaining messages: %d %v", n, err)
	}
	if _, err := s.MessageByID(start.ID); err != nil {
		t.Fatalf("parent message: %v", err)
	}
}

func TestDeleteThreadRollback(t *testing.T) {
	s, start := threadStore(t)
	th, err := s.CreateThread(1, start.ID, "Rollback", "bob", nil, t0)
	if err != nil {
		t.Fatal(err)
	}
	up, err := s.AddUpload(Upload{Key: "keep-file", Name: "a.png", Type: "image/png", Size: 1, Author: "bob"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.AddMessage(NewMessage{ChannelID: th.ID, Author: "bob", Text: "keep", At: t0, Uploads: []Attachment{{ID: up.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Follow(20, th.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReadMark(20, th.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	// Fail at the last step, after messages, uploads, follows and marks go.
	if _, err := s.db.Exec(`CREATE TRIGGER fail_thread_rename BEFORE UPDATE OF name ON channels
		BEGIN SELECT RAISE(ABORT, 'forced rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if keys, err := s.DeleteThread(th.ID); err == nil || len(keys) != 0 {
		t.Fatalf("failed deletion returned %v %v", keys, err)
	}
	if got, err := s.MessageByID(m.ID); err != nil || len(got.Uploads) != 1 || got.Uploads[0].Key != up.Key {
		t.Fatalf("message after rollback: %+v %v", got, err)
	}
	if got, err := s.Thread(th.ID); err != nil || got.Replies != 1 {
		t.Fatalf("thread after rollback: %+v %v", got, err)
	}
	if got, err := s.Followed(20); err != nil || !slices.Equal(got, []int64{th.ID}) {
		t.Fatalf("follows: %v %v", got, err)
	}
	if got, err := s.ReadMarks(20); err != nil || len(got) != 1 {
		t.Fatalf("marks: %v %v", got, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_thread_rename`); err != nil {
		t.Fatal(err)
	}
	if keys, err := s.DeleteThread(th.ID); err != nil || !slices.Equal(keys, []string{up.Key}) {
		t.Fatalf("retry deletion: %v %v", keys, err)
	}
}
