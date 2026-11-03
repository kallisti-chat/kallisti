package store

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func channelNames(cs []Channel) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func TestEnsureChannelsIsIdempotentAndOrdered(t *testing.T) {
	s := open(t)
	if err := s.EnsureChannels([]string{"general", "links"}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureChannels([]string{"links", "general", "memes"}); err != nil {
		t.Fatal(err)
	}
	cs, err := s.Channels()
	if err != nil {
		t.Fatal(err)
	}
	// The latest configuration decides the order. Removed names stay hidden.
	if got := channelNames(cs); len(got) != 3 || got[0] != "links" || got[1] != "general" || got[2] != "memes" {
		t.Fatalf("got %v", got)
	}
	s.EnsureChannels([]string{"general"})
	cs, _ = s.Channels()
	if got := channelNames(cs); len(got) != 1 || got[0] != "general" {
		t.Fatalf("after removal: got %v", got)
	}
}

func TestMessagesAndHistory(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general", "links"})
	cs, _ := s.Channels()
	general, links := cs[0].ID, cs[1].ID
	base := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	var ids []int64
	for i := 0; i < 5; i++ {
		m, err := s.AddMessage(NewMessage{ChannelID: general, Author: "alice", Text: string(rune('a' + i)), At: base.Add(time.Duration(i) * time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	s.AddMessage(NewMessage{ChannelID: links, Author: "bob", Text: "elsewhere", At: base})

	newest, err := s.History(general, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(newest) != 2 || newest[0].Text != "d" || newest[1].Text != "e" {
		t.Fatalf("newest page: %+v", newest)
	}
	older, _ := s.History(general, newest[0].ID, 10)
	if len(older) != 3 || older[0].Text != "a" || older[2].Text != "c" {
		t.Fatalf("older page: %+v", older)
	}
	if !older[0].CreatedAt.Equal(base) || older[0].Author != "alice" || older[0].ChannelID != general {
		t.Fatalf("fields: %+v", older[0])
	}
}

func TestAddMessageToUnknownChannelFails(t *testing.T) {
	s := open(t)
	if _, err := s.AddMessage(NewMessage{ChannelID: 999, Author: "alice", Text: "x", At: time.Now()}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUploadsAndAttachments(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	cat := Media{Width: 640, Height: 480, Duration: 1500}
	u, err := s.AddUpload(Upload{Key: "k1", Name: "cat.png", Type: "image/png", Size: 1234, Author: "alice", Media: cat}, at)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.UploadByKey("k1")
	if err != nil || got.ID != u.ID || got.Name != "cat.png" || got.Author != "alice" || got.Used || got.Media != cat {
		t.Fatalf("got %+v %v", got, err)
	}
	m, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", Text: "look", At: at, Uploads: []Attachment{{ID: u.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Uploads) != 1 || m.Uploads[0].Name != "cat.png" {
		t.Fatalf("message upload: %+v", m.Uploads)
	}
	got, _ = s.UploadByKey("k1")
	if !got.Used {
		t.Fatal("upload not marked as used")
	}
	// An upload can be attached only once.
	if _, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", Text: "again", At: at, Uploads: []Attachment{{ID: u.ID}}}); err == nil {
		t.Fatal("expected an error for a used upload")
	}
	hist, _ := s.History(1, 0, 10)
	if len(hist) != 1 || len(hist[0].Uploads) != 1 || hist[0].Uploads[0].Key != "k1" || hist[0].Uploads[0].Size != 1234 || hist[0].Uploads[0].Media != cat {
		t.Fatalf("history: %+v", hist)
	}
	if _, err := s.UploadByKey("nope"); err == nil {
		t.Fatal("expected an error for a missing upload")
	}
}

func TestMigratesAnOldDatabase(t *testing.T) {
	path := t.TempDir() + "/old.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the first schema: no upload column.
	if _, err := s.db.Exec(`ALTER TABLE messages DROP COLUMN upload_id`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	s.EnsureChannels([]string{"general"})
	if _, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", Text: "hi", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureUploadsOnce(t *testing.T) {
	path := t.TempDir() + "/old.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	s.AddUpload(Upload{Key: "video", Name: "a.mp4", Type: "video/mp4", Size: 1, Author: "a"}, at)
	s.AddUpload(Upload{Key: "file", Name: "b.zip", Type: "application/zip", Size: 1, Author: "a"}, at)
	// Uploads from before sizes were kept.
	for _, q := range []string{`ALTER TABLE uploads DROP COLUMN width`, `ALTER TABLE uploads DROP COLUMN height`,
		`ALTER TABLE uploads DROP COLUMN duration`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	video := Media{Width: 1280, Height: 720, Duration: 2500}
	var asked []string
	measure := func(u Upload) Media {
		asked = append(asked, u.Key+" "+u.Type+" "+u.Name)
		if u.Key == "video" {
			return video
		}
		return Media{}
	}
	if err := s.MeasureUploads(measure); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked, []string{"video video/mp4 a.mp4", "file application/zip b.zip"}) {
		t.Fatalf("asked %v", asked)
	}
	if u, _ := s.UploadByKey("video"); u.Media != video {
		t.Fatalf("video: %+v", u)
	}
	if u, _ := s.UploadByKey("file"); u.Media != (Media{}) {
		t.Fatalf("file: %+v", u)
	}
	asked = nil
	if err := s.MeasureUploads(measure); err != nil || asked != nil {
		t.Fatalf("again: %v %v", asked, err)
	}
}

func TestDeleteUnusedUploads(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	old := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	stale, _ := s.AddUpload(Upload{Key: "stale", Name: "a", Type: "x", Size: 1, Author: "a"}, old)
	attached, _ := s.AddUpload(Upload{Key: "attached", Name: "b", Type: "x", Size: 1, Author: "a"}, old)
	s.AddMessage(NewMessage{ChannelID: 1, Author: "a", Text: "", At: old, Uploads: []Attachment{{ID: attached.ID}}})
	s.AddUpload(Upload{Key: "fresh", Name: "c", Type: "x", Size: 1, Author: "a"}, old.Add(time.Hour))
	keys, err := s.DeleteUnusedUploads(old.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "stale" {
		t.Fatalf("deleted %v", keys)
	}
	if _, err := s.UploadByKey("stale"); err == nil {
		t.Fatal("stale upload still there")
	}
	_ = stale
	for _, k := range []string{"attached", "fresh"} {
		if _, err := s.UploadByKey(k); err != nil {
			t.Fatalf("%s was deleted", k)
		}
	}
}

func TestVerifiedAuthors(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", Verified: true, Text: "a", At: time.Now()})
	s.AddMessage(NewMessage{ChannelID: 1, Author: "bob", Text: "b", At: time.Now()})
	hist, _ := s.History(1, 0, 10)
	if !hist[0].Verified || hist[1].Verified {
		t.Fatalf("got %+v", hist)
	}
}

func TestUploadedBytesSince(t *testing.T) {
	s := open(t)
	day := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	s.AddUpload(Upload{Key: "a", Name: "a", Type: "x", Size: 100, Author: "alice"}, day.Add(-48*time.Hour))
	s.AddUpload(Upload{Key: "b", Name: "b", Type: "x", Size: 200, Author: "alice"}, day)
	s.AddUpload(Upload{Key: "c", Name: "c", Type: "x", Size: 300, Author: "alice"}, day.Add(time.Hour))
	s.AddUpload(Upload{Key: "d", Name: "d", Type: "x", Size: 999, Author: "bob"}, day)
	n, err := s.UploadedBytesSince("alice", day.Add(-24*time.Hour))
	if err != nil || n != 500 {
		t.Fatalf("got %d %v, want 500", n, err)
	}
}

func TestAccounts(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	a, err := s.CreateAccount(Account{Name: "Alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if err != nil || a.ID == 0 {
		t.Fatalf("create: %+v %v", a, err)
	}
	if _, err := s.CreateAccount(Account{Name: "alice", PasswordHash: "h2", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, ""); err != ErrNameTaken {
		t.Fatalf("duplicate (other case): got %v, want ErrNameTaken", err)
	}
	got, err := s.AccountByName("ALICE")
	if err != nil || got.ID != a.ID || got.Name != "Alice" || string(got.CertPEM) != "c" || got.PasswordHash != "h" {
		t.Fatalf("by name: %+v %v", got, err)
	}
	if _, err := s.AccountByName("bob"); err != ErrNotFound {
		t.Fatalf("missing: got %v", err)
	}
}

func TestSessions(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	a, err := s.CreateAccount(Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("hash1", a.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := s.AccountBySession("hash1", now)
	if err != nil || got.ID != a.ID {
		t.Fatalf("session: %+v %v", got, err)
	}
	if _, err := s.AccountBySession("hash1", now.Add(2*time.Hour)); err != ErrNotFound {
		t.Fatalf("expired session: got %v", err)
	}
	if err := s.DeleteSession("hash1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountBySession("hash1", now); err != ErrNotFound {
		t.Fatalf("deleted session: got %v", err)
	}
}

func TestAccountNamesCompareByKey(t *testing.T) {
	s := open(t)
	now := time.Now()
	if _, err := s.CreateAccount(Account{Name: "Émile", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, ""); err != nil {
		t.Fatal(err)
	}
	// Lower case and decomposed: the same name.
	if _, err := s.CreateAccount(Account{Name: "émile", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, ""); err != ErrNameTaken {
		t.Fatalf("got %v, want ErrNameTaken", err)
	}
	if a, err := s.AccountByName(" ÉMILE "); err != nil || a.Name != "Émile" {
		t.Fatalf("by name: %+v %v", a, err)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	a, _ := s.CreateAccount(Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	s.CreateSession("old", a.ID, now.Add(-time.Minute))
	s.CreateSession("new", a.ID, now.Add(time.Hour))
	if n, err := s.DeleteExpiredSessions(now); err != nil || n != 1 {
		t.Fatalf("deleted %d %v", n, err)
	}
	if _, err := s.AccountBySession("new", now); err != nil {
		t.Fatal("valid session deleted")
	}
}

func TestPasswordAndSessionsOfAnAccount(t *testing.T) {
	s := open(t)
	now := time.Now()
	a, _ := s.CreateAccount(Account{Name: "alice", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	s.CreateSession("one", a.ID, now.Add(time.Hour))
	s.CreateSession("two", a.ID, now.Add(time.Hour))
	if err := s.ChangePassword(a.ID, "two", "stale", "h2", now); err != ErrNotFound {
		t.Fatalf("change from a stale hash: %v", err)
	}
	if err := s.ChangePassword(a.ID, "gone", "h", "h2", now); err != ErrNotFound {
		t.Fatalf("change from an ended session: %v", err)
	}
	if err := s.ChangePassword(a.ID, "two", "h", "h2", now.Add(2*time.Hour)); err != ErrNotFound {
		t.Fatalf("change from an expired session: %v", err)
	}
	if got, _ := s.AccountByName("alice"); got.PasswordHash != "h" {
		t.Fatalf("refused changes kept hash %q", got.PasswordHash)
	}
	if _, err := s.AccountBySession("one", now); err != nil {
		t.Fatal("a refused change ended session one")
	}
	if err := s.ChangePassword(a.ID, "two", "h", "h2", now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AccountByName("alice"); got.PasswordHash != "h2" {
		t.Fatalf("hash %q", got.PasswordHash)
	}
	if _, err := s.AccountBySession("one", now); err != ErrNotFound {
		t.Error("session one still valid")
	}
	if _, err := s.AccountBySession("two", now); err != nil {
		t.Error("session two was deleted")
	}
	if err := s.DeleteSessionsExcept(a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountBySession("two", now); err != ErrNotFound {
		t.Error("session two still valid after deleting all")
	}
}

func TestTheDatabaseIsShared(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.DB().Ping(); err != nil {
		t.Fatal(err)
	}
}

func TestVoiceChannelChats(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.EnsureChannels([]string{"general"})
	id, err := s.VoiceChat(5)
	if err != nil || id == 0 {
		t.Fatalf("id %d err %v", id, err)
	}
	again, _ := s.VoiceChat(5)
	other, _ := s.VoiceChat(6)
	if again != id || other == id {
		t.Fatalf("ids %d %d %d", id, again, other)
	}
	if voice, ok, _ := s.VoiceOf(id); !ok || voice != 5 {
		t.Fatalf("voice of %d: %d %v", id, voice, ok)
	}
	if _, ok, _ := s.VoiceOf(1); ok {
		t.Fatal("a text channel is a voice chat")
	}
	// Voice chats are not text channels, also after a restart.
	s.EnsureChannels([]string{"general"})
	chans, _ := s.Channels()
	if len(chans) != 1 || chans[0].Name != "general" {
		t.Fatalf("channels %+v", chans)
	}
	// Messages work as in text channels.
	if _, err := s.AddMessage(NewMessage{ChannelID: id, Author: "alice", Text: "hi", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if msgs, _ := s.History(id, 0, 10); len(msgs) != 1 {
		t.Fatalf("history %+v", msgs)
	}
}

func TestManagingTextChannels(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// The configured channels only seed a new server.
	if err := s.SeedChannels([]string{"general", "links"}); err != nil {
		t.Fatal(err)
	}
	s.SeedChannels([]string{"other"})
	names := func() []string {
		chans, _ := s.Channels()
		out := []string{}
		for _, c := range chans {
			out = append(out, c.Name)
		}
		return out
	}
	if got := names(); !slices.Equal(got, []string{"general", "links"}) {
		t.Fatalf("seeded %v", got)
	}
	games, err := s.CreateChannel("games", 0, true)
	if err != nil || games.Name != "games" {
		t.Fatalf("create %+v %v", games, err)
	}
	if _, err := s.CreateChannel("games", 0, true); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := s.RenameChannel(games.ID, "gaming"); err != nil {
		t.Fatal(err)
	}
	chans, _ := s.Channels()
	if err := s.SetPositions([]int64{games.ID, chans[0].ID, chans[1].ID}); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"gaming", "general", "links"}) {
		t.Fatalf("after moves %v", got)
	}
	if err := s.HideChannel(games.ID); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"general", "links"}) {
		t.Fatalf("after delete %v", got)
	}
	if back, err := s.CreateChannel("gaming", 0, true); err != nil || back.ID != games.ID {
		t.Fatalf("made again: %+v %v (want the old channel back)", back, err)
	}
	// Voice channel chats cannot be renamed or hidden as text channels.
	voice, _ := s.VoiceChat(3)
	if err := s.RenameChannel(voice, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("renamed a voice chat: %v", err)
	}
}

func TestTextChannelNamesCannotLookLikeVoiceChats(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateChannel("voice:7", 0, true); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("create: %v", err)
	}
	if err := s.EnsureChannels([]string{"voice:7"}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("ensure: %v", err)
	}
	// Configured channels never show a voice chat.
	id, _ := s.VoiceChat(7)
	s.EnsureChannels([]string{"general"})
	if chans, _ := s.Channels(); len(chans) != 1 {
		t.Fatalf("channels %+v", chans)
	}
	if voice, ok, _ := s.VoiceOf(id); !ok || voice != 7 {
		t.Fatal("voice chat lost")
	}
}

func TestTextChannelsRememberTheirMumbleChannel(t *testing.T) {
	s := open(t)
	if err := s.EnsureChannels([]string{"general"}); err != nil {
		t.Fatal(err)
	}
	chans, _ := s.Channels()
	if chans[0].Mumble != 0 {
		t.Fatalf("new channel: %+v", chans[0])
	}
	if err := s.SetMumbleChannel(chans[0].ID, 42); err != nil {
		t.Fatal(err)
	}
	chans, _ = s.Channels()
	if chans[0].Mumble != 42 {
		t.Fatalf("after set: %+v", chans[0])
	}
}

func TestEditAndDeleteMessages(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	chans, _ := s.Channels()
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	m, _ := s.AddMessage(NewMessage{ChannelID: chans[0].ID, Author: "alice", Verified: true, Text: "helo", At: at})
	edited, err := s.EditMessage(m.ID, "hello", "", at.Add(time.Minute))
	if err != nil || edited.Text != "hello" || !edited.EditedAt.Equal(at.Add(time.Minute)) || edited.Author != "alice" {
		t.Fatalf("edit: %+v %v", edited, err)
	}
	all, _ := s.AddMessage(NewMessage{ChannelID: chans[0].ID, Author: "mod", Text: "@here hi", At: at, MentionAll: "here"})
	if got, _ := s.MessageByID(all.ID); got.MentionAll != "here" || all.MentionAll != "here" {
		t.Fatalf("mention all: %+v", got)
	}
	if got, _ := s.EditMessage(all.ID, "hi", "", at); got.MentionAll != "" {
		t.Fatalf("mention all after edit: %+v", got)
	}
	s.DeleteMessage(all.ID)
	hist, _ := s.History(chans[0].ID, 0, 10)
	if hist[0].Text != "hello" || hist[0].EditedAt.IsZero() {
		t.Fatalf("history: %+v", hist[0])
	}
	got, err := s.MessageByID(m.ID)
	if err != nil || got.Text != "hello" {
		t.Fatalf("by id: %+v %v", got, err)
	}
	// Deleting a message with an attachment deletes the upload too.
	u, _ := s.AddUpload(Upload{Key: "k9", Name: "a.png", Type: "image/png", Size: 1, Author: "alice"}, at)
	withFile, _ := s.AddMessage(NewMessage{ChannelID: chans[0].ID, Author: "alice", Text: "", At: at, Uploads: []Attachment{{ID: u.ID}}})
	gone, err := s.DeleteMessage(withFile.ID)
	if err != nil || len(gone.Uploads) != 1 || gone.Uploads[0].Key != "k9" {
		t.Fatalf("delete: %+v %v", gone, err)
	}
	if _, err := s.UploadByKey("k9"); err == nil {
		t.Fatal("upload still there")
	}
	if _, err := s.MessageByID(withFile.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted message: %v", err)
	}
	if _, err := s.EditMessage(withFile.ID, "x", "", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit deleted: %v", err)
	}
}

func TestDeletingTwiceFails(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	chans, _ := s.Channels()
	m, _ := s.AddMessage(NewMessage{ChannelID: chans[0].ID, Author: "a", Text: "x", At: time.Now()})
	if _, err := s.DeleteMessage(m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteMessage(m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestRecentMessagesAndDeletingMany(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	chans, _ := s.Channels()
	voice, _ := s.VoiceChat(5)
	dm, _ := s.DirectChat(7, 8)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	add := func(ch int64, author uint32, verified bool, ago time.Duration, uploads ...Attachment) int64 {
		m, err := s.AddMessage(NewMessage{ChannelID: ch, Author: "x", Verified: verified, AuthorID: author, Text: "x", At: now.Add(-ago), Uploads: uploads})
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	u, _ := s.AddUpload(Upload{Key: "k1", Name: "a.png", Type: "image/png", Size: 1, Author: "x"}, now)
	old := add(chans[0].ID, 7, true, 2*time.Hour)
	text := add(chans[0].ID, 7, true, time.Minute, Attachment{ID: u.ID})
	inVoice := add(voice, 7, true, 0)
	add(dm, 7, true, 0)                    // direct chats are left out
	other := add(chans[0].ID, 8, true, 0)  // another user
	guest := add(chans[0].ID, 0, false, 0) // a guest
	guestOld := add(voice, 0, false, 3*time.Hour)
	since := now.Add(-time.Hour)
	if ids, err := s.RecentByUser(7, since, 10); err != nil || fmt.Sprint(ids) != fmt.Sprint([]int64{inVoice, text}) {
		t.Fatalf("by user: %v %v", ids, err)
	}
	if ids, _ := s.RecentByUser(7, now.Add(-3*time.Hour), 2); fmt.Sprint(ids) != fmt.Sprint([]int64{inVoice, text}) {
		t.Fatalf("limit: %v", ids)
	}
	if ids, _ := s.RecentAmong([]int64{guest, guestOld, other}, since, 10); fmt.Sprint(ids) != fmt.Sprint([]int64{guest, other}) {
		t.Fatalf("among: %v", ids)
	}
	gone, err := s.DeleteMessages([]int64{text, inVoice, 9999})
	if err != nil || len(gone) != 2 || gone[0].ID != text || len(gone[0].Uploads) != 1 || gone[0].Uploads[0].Key != "k1" || gone[1].ChannelID != voice {
		t.Fatalf("deleted %+v %v", gone, err)
	}
	if _, err := s.UploadByKey("k1"); err == nil {
		t.Fatal("upload still there")
	}
	if ids, _ := s.RecentByUser(7, now.Add(-3*time.Hour), 10); fmt.Sprint(ids) != fmt.Sprint([]int64{old}) {
		t.Fatalf("after delete: %v", ids)
	}
	if gone, err := s.DeleteMessages(nil); err != nil || len(gone) != 0 {
		t.Fatalf("none: %v %v", gone, err)
	}
}

func TestReadMarks(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general", "random"})
	chans, _ := s.Channels()
	if err := s.SetReadMark(1, chans[0].ID, 5); err != nil {
		t.Fatal(err)
	}
	s.SetReadMark(1, chans[0].ID, 3) // never back
	s.SetReadMark(1, chans[1].ID, 2)
	s.SetReadMark(2, chans[0].ID, 9)
	got, err := s.ReadMarks(1)
	if err != nil || got[chans[0].ID] != 5 || got[chans[1].ID] != 2 || len(got) != 2 {
		t.Fatalf("marks %v %v", got, err)
	}
}

func TestMessagesAfter(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	chans, _ := s.Channels()
	var ids []int64
	for _, text := range []string{"a", "@alice hi", "b", "@ALICE again"} {
		m, _ := s.AddMessage(NewMessage{ChannelID: chans[0].ID, Author: "bob", Text: text, At: time.Now()})
		ids = append(ids, m.ID)
	}
	latest, texts, err := s.MessagesAfter(chans[0].ID, ids[1], "@")
	if err != nil || latest != ids[3] || len(texts) != 1 || texts[0].Text != "@ALICE again" {
		t.Fatalf("after: %v %+v %v", latest, texts, err)
	}
	if latest, _, _ := s.MessagesAfter(chans[0].ID, 0, "@"); latest != ids[3] {
		t.Fatalf("latest %v", latest)
	}
}

func TestVoiceChatsList(t *testing.T) {
	s := open(t)
	a, _ := s.VoiceChat(7)
	b, _ := s.VoiceChat(9)
	got, err := s.VoiceChats()
	if err != nil || got[a] != 7 || got[b] != 9 || len(got) != 2 {
		t.Fatalf("voice chats %v %v", got, err)
	}
}

func TestChannelTopics(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	chans, _ := s.Channels()
	if chans[0].Topic != "" {
		t.Fatalf("new topic %q", chans[0].Topic)
	}
	if err := s.SetTopic(chans[0].ID, "Anything goes"); err != nil {
		t.Fatal(err)
	}
	chans, _ = s.Channels()
	if chans[0].Topic != "Anything goes" {
		t.Fatalf("topic %q", chans[0].Topic)
	}
	if err := s.SetTopic(999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown channel: %v", err)
	}
}

func TestChannelSlowMode(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	chans, _ := s.Channels()
	if chans[0].SlowMode != 0 {
		t.Fatalf("new slow mode %d", chans[0].SlowMode)
	}
	if err := s.SetSlowMode(chans[0].ID, 30); err != nil {
		t.Fatal(err)
	}
	if chans, _ = s.Channels(); chans[0].SlowMode != 30 {
		t.Fatalf("slow mode %d", chans[0].SlowMode)
	}
	if err := s.SetSlowMode(999, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown channel: %v", err)
	}
}

func TestReplies(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general", "random"})
	chans, _ := s.Channels()
	at := time.Now()
	orig, _ := s.AddMessage(NewMessage{ChannelID: chans[0].ID, Author: "alice", Text: "a long original message here", At: at})
	reply, err := s.AddMessage(NewMessage{ChannelID: chans[0].ID, Author: "bob", Text: "yes", At: at, ReplyTo: orig.ID})
	if err != nil || reply.Reply == nil || reply.Reply.ID != orig.ID || reply.Reply.Author != "alice" || reply.Reply.Text != orig.Text {
		t.Fatalf("reply %+v %v", reply.Reply, err)
	}
	hist, _ := s.History(chans[0].ID, 0, 10)
	if hist[1].Reply == nil || hist[1].Reply.Author != "alice" {
		t.Fatalf("history %+v", hist[1])
	}
	// Only to a message in the same channel.
	if _, err := s.AddMessage(NewMessage{ChannelID: chans[1].ID, Author: "bob", Text: "x", At: at, ReplyTo: orig.ID}); err == nil {
		t.Fatal("replied across channels")
	}
	// The original goes: the reply stays, its quote is gone.
	s.DeleteMessage(orig.ID)
	hist, _ = s.History(chans[0].ID, 0, 10)
	if hist[0].Reply == nil || hist[0].Reply.Author != "" || hist[0].Reply.ID != orig.ID {
		t.Fatalf("after delete %+v", hist[0].Reply)
	}
}

func TestSearch(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general", "random"})
	chans, _ := s.Channels()
	g, r := chans[0].ID, chans[1].ID
	at := time.Now()
	add := func(ch int64, author, text string) Message {
		m, _ := s.AddMessage(NewMessage{ChannelID: ch, Author: author, Text: text, At: at})
		return m
	}
	add(g, "alice", "Pizza tonight at eight")
	add(g, "bob", "I prefer pasta")
	pizza := add(r, "bob", "pizzas are great")
	add(g, "carol", `quotes "and" stars* (parens) AND OR`)
	ids := func(ms []Message) (out []int64) {
		for _, m := range ms {
			out = append(out, m.ID)
		}
		return out
	}
	got, err := s.Search("pizz", []int64{g, r}, "", 10)
	if err != nil || len(got) != 2 || got[0].ID != pizza.ID {
		t.Fatalf("pizz: %v %v", ids(got), err) // prefix match, newest first
	}
	if got, _ := s.Search("pizza", []int64{g}, "", 10); len(got) != 1 {
		t.Fatalf("only general: %v", ids(got))
	}
	if got, _ := s.Search("pizza", []int64{g, r}, "BOB", 10); len(got) != 1 || got[0].Author != "bob" {
		t.Fatalf("by bob: %v", ids(got))
	}
	// FTS syntax in the query is just text.
	if got, err := s.Search(`"and" stars* (parens`, []int64{g}, "", 10); err != nil || len(got) != 1 {
		t.Fatalf("syntax: %v %v", ids(got), err)
	}
	// Edits and deletes reach the index.
	s.EditMessage(pizza.ID, "burgers", "", at)
	if got, _ := s.Search("pizzas", []int64{r}, "", 10); len(got) != 0 {
		t.Fatalf("after edit: %v", ids(got))
	}
	m := add(g, "dave", "unique words")
	s.DeleteMessage(m.ID)
	if got, _ := s.Search("unique", []int64{g}, "", 10); len(got) != 0 {
		t.Fatalf("after delete: %v", ids(got))
	}
	// No words: all messages (by the author), newest first.
	if got, _ := s.Search("   ", []int64{g}, "alice", 10); len(got) != 1 || got[0].Author != "alice" {
		t.Fatalf("no words, by alice: %v", ids(got))
	}
	if got, _ := s.Search("", []int64{r}, "", 10); len(got) != 1 {
		t.Fatalf("no words, in random: %v", ids(got))
	}
}

func TestFTSQuery(t *testing.T) {
	for in, want := range map[string]string{
		"pizza night": `"pizza"* "night"*`,
		"a pizza":     `"a" "pizza"*`, // one letter: exact, not every word with a
		"nul\x00byte": `"nulbyte"*`,
		`say "hi"`:    `"say"* """hi"""*`,
		"-- ** ??":    ``,
	} {
		if got := ftsQuery(in); got != want {
			t.Errorf("%q: %s, want %s", in, got, want)
		}
	}
}

func TestFindVoiceChatDoesNotMakeOne(t *testing.T) {
	s := open(t)
	if _, ok, err := s.FindVoiceChat(5); ok || err != nil {
		t.Fatalf("found one: %v %v", ok, err)
	}
	made, _ := s.VoiceChat(5)
	if id, ok, err := s.FindVoiceChat(5); !ok || err != nil || id != made {
		t.Fatalf("got %d %v %v, want %d", id, ok, err, made)
	}
	if chats, _ := s.VoiceChats(); len(chats) != 1 {
		t.Fatalf("chats %v", chats)
	}
}

func TestCountAfter(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	var ids []int64
	for range 4 {
		m, _ := s.AddMessage(NewMessage{ChannelID: 1, Author: "a", Text: "x", At: time.Now()})
		ids = append(ids, m.ID)
	}
	if n, err := s.CountAfter(1, ids[1]); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
}

func TestMessageIDsAreNotReused(t *testing.T) {
	path := t.TempDir() + "/ids.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.EnsureChannels([]string{"general"})
	add := func(s *Store, text string) Message {
		t.Helper()
		m, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "bob", Text: text, At: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	first := add(s, "hi")
	s.SetReadMark(1, 1, first.ID)
	if _, err := s.DeleteMessage(first.ID); err != nil {
		t.Fatal(err)
	}
	m := add(s, "@alice there")
	if _, msgs, _ := s.MessagesAfter(1, first.ID, "@"); m.ID <= first.ID || len(msgs) != 1 {
		t.Fatalf("new id %d after %d: unread %+v", m.ID, first.ID, msgs)
	}
	// Also after a restart.
	s.DeleteMessage(m.ID)
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if again := add(s, "x"); again.ID <= m.ID {
		t.Fatalf("id %d reused after restart (last %d)", again.ID, m.ID)
	}
}

func TestMessageIDCounterStartsAtTheNewestMessage(t *testing.T) {
	path := t.TempDir() + "/old.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.EnsureChannels([]string{"general"})
	for range 2 {
		s.AddMessage(NewMessage{ChannelID: 1, Author: "a", Text: "x", At: time.Now()})
	}
	// A database from before the counter.
	if _, err := s.db.Exec(`DROP TABLE store_meta`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if m, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "a", Text: "x", At: time.Now()}); err != nil || m.ID != 3 {
		t.Fatalf("id %d %v", m.ID, err)
	}
}

func TestUploadPreviews(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	at := time.Now()
	video, _ := s.AddUpload(Upload{Key: "video", Name: "a.mp4", Type: "video/mp4", Size: 1, Author: "a"}, at)
	image, _ := s.AddUpload(Upload{Key: "image", Name: "b.png", Type: "image/png", Size: 1, Author: "a"}, at)
	s.AddUpload(Upload{Key: "unused", Name: "c.png", Type: "image/png", Size: 1, Author: "a"}, at)
	zip, _ := s.AddUpload(Upload{Key: "zip", Name: "d.zip", Type: "application/zip", Size: 1, Author: "a"}, at)
	for _, u := range []Upload{video, image, zip} {
		s.AddMessage(NewMessage{ChannelID: 1, Author: "a", At: at, Uploads: []Attachment{{ID: u.ID}}})
	}
	todo, err := s.UploadsWithoutPreview()
	if err != nil || len(todo) != 2 || todo[0].Key != "image" || todo[1].Key != "video" {
		t.Fatalf("without preview (newest first, attached pictures and videos): %+v %v", todo, err)
	}
	if found, err := s.SetUploadPreview("video", "image/webp"); !found || err != nil {
		t.Fatalf("set: %v %v", found, err)
	}
	s.SetUploadPreview("image", NoPreview)
	if found, _ := s.SetUploadPreview("gone", "image/webp"); found {
		t.Fatal("set for a missing upload")
	}
	if u, _ := s.UploadByKey("video"); u.Preview != "image/webp" {
		t.Fatalf("upload: %+v", u)
	}
	m, err := s.MessageByUpload("video")
	if err != nil || len(m.Uploads) != 1 || m.Uploads[0].Preview != "image/webp" {
		t.Fatalf("message: %+v %v", m, err)
	}
	if _, err := s.MessageByUpload("unused"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unattached: %v", err)
	}
	if todo, _ := s.UploadsWithoutPreview(); len(todo) != 0 {
		t.Fatalf("still without preview: %+v", todo)
	}
}

func TestSeveralUploadsPerMessage(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	var ups []Upload
	for _, k := range []string{"a", "b", "c", "d"} {
		u, _ := s.AddUpload(Upload{Key: k, Name: k + ".png", Type: "image/png", Size: 1, Author: "alice"}, at)
		ups = append(ups, u)
	}
	// In the order given, with descriptions.
	m, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", At: at, Uploads: []Attachment{
		{ID: ups[2].ID}, {ID: ups[0].ID, Description: "a cat"}}})
	if err != nil {
		t.Fatal(err)
	}
	keys := func(us []Upload) string {
		out := ""
		for _, u := range us {
			out += fmt.Sprintf("%s:%s ", u.Key, u.Description)
		}
		return out
	}
	const want = "c: a:a cat "
	if keys(m.Uploads) != want {
		t.Fatalf("added: %s", keys(m.Uploads))
	}
	for _, get := range []func() ([]Message, error){
		func() ([]Message, error) { return s.History(1, 0, 10) },
		func() ([]Message, error) { m, err := s.MessageByID(m.ID); return []Message{m}, err },
		func() ([]Message, error) { return s.Search("", []int64{1}, "alice", 10) },
	} {
		if got, err := get(); err != nil || len(got) != 1 || keys(got[0].Uploads) != want {
			t.Fatalf("read back: %+v %v", got, err)
		}
	}
	if byUpload, err := s.MessageByUpload("a"); err != nil || byUpload.ID != m.ID {
		t.Fatalf("by upload: %+v %v", byUpload, err)
	}
	// A used upload among others: nothing is attached.
	if _, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", At: at, Uploads: []Attachment{{ID: ups[3].ID}, {ID: ups[0].ID}}}); err == nil {
		t.Fatal("a used upload was attached again")
	}
	if u, _ := s.UploadByKey("d"); u.Used {
		t.Fatal("the free upload was used up by a refused message")
	}
	// The same upload twice is refused.
	if _, err := s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", At: at, Uploads: []Attachment{{ID: ups[3].ID}, {ID: ups[3].ID}}}); err == nil {
		t.Fatal("one upload attached twice")
	}
	gone, err := s.DeleteMessage(m.ID)
	if err != nil || keys(gone.Uploads) != want {
		t.Fatalf("deleted: %+v %v", gone, err)
	}
	for _, k := range []string{"a", "c"} {
		if _, err := s.UploadByKey(k); err == nil {
			t.Fatalf("upload %s still there", k)
		}
	}
}

// Messages from before the list kept their one upload in messages.upload_id.
func TestMigratesSingleUploads(t *testing.T) {
	path := t.TempDir() + "/old.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.EnsureChannels([]string{"general"})
	u, _ := s.AddUpload(Upload{Key: "old", Name: "old.png", Type: "image/png", Size: 1, Author: "alice"}, time.Now())
	for _, q := range []string{
		`UPDATE uploads SET used = 1`,
		`INSERT INTO messages (id, channel_id, author, text, created_at, upload_id) VALUES (1, 1, 'alice', '', 0, ` + fmt.Sprint(u.ID) + `)`,
		`DELETE FROM store_meta WHERE key = 'upload_messages'`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // the second time changes nothing
		s.Close()
		if s, err = Open(path); err != nil {
			t.Fatal(err)
		}
		if m, err := s.MessageByID(1); err != nil || len(m.Uploads) != 1 || m.Uploads[0].Key != "old" {
			t.Fatalf("%+v %v", m, err)
		}
	}
	defer s.Close()
	var old int
	if s.db.QueryRow(`SELECT count(*) FROM messages WHERE upload_id IS NOT NULL`).Scan(&old); old != 0 {
		t.Fatalf("upload_id still set: %d", old)
	}
}

// An edit can take files off its message (their rows go; the caller deletes
// the files).
func TestEditRemovesUploads(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	var atts []Attachment
	for _, k := range []string{"a", "b", "c"} {
		u, _ := s.AddUpload(Upload{Key: k, Name: k, Type: "image/png", Size: 1, Author: "alice"}, at)
		atts = append(atts, Attachment{ID: u.ID})
	}
	m, _ := s.AddMessage(NewMessage{ChannelID: 1, Author: "alice", Text: "x", At: at, Uploads: atts})
	other, _ := s.AddUpload(Upload{Key: "z", Name: "z", Type: "image/png", Size: 1, Author: "alice"}, at)
	edited, err := s.EditMessage(m.ID, "y", "", at.Add(time.Minute), atts[1].ID, other.ID)
	if err != nil || edited.Text != "y" || len(edited.Uploads) != 2 || edited.Uploads[0].Key != "a" || edited.Uploads[1].Key != "c" {
		t.Fatalf("edited %+v %v", edited, err)
	}
	if _, err := s.UploadByKey("b"); err == nil {
		t.Fatal("removed upload still there")
	}
	if _, err := s.UploadByKey("z"); err != nil {
		t.Fatal("another message's (or no message's) upload went too")
	}
	// A message keeps text or a file, also when edits cross.
	if _, err := s.EditMessage(m.ID, "", "", at, atts[0].ID, atts[2].ID); !errors.Is(err, ErrEmpty) {
		t.Fatalf("emptied: %v", err)
	}
	if got, _ := s.MessageByID(m.ID); got.Text != "y" || len(got.Uploads) != 2 {
		t.Fatalf("after a refused edit: %+v", got)
	}
}

// Mumble's catch-up of missed direct messages shows their files as links.
func TestMessagesAfterHaveTheirFiles(t *testing.T) {
	s := open(t)
	s.EnsureChannels([]string{"general"})
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	u, _ := s.AddUpload(Upload{Key: "f", Name: "f.png", Type: "image/png", Size: 1, Author: "a"}, at)
	m, _ := s.AddMessage(NewMessage{ChannelID: 1, Author: "a", At: at, ReplyTo: 0, Text: "", Uploads: []Attachment{{ID: u.ID}}})
	_, got, err := s.MessagesAfter(1, 0, "")
	if err != nil || len(got) != 1 || got[0].ID != m.ID || len(got[0].Uploads) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSessionExpires(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	acc, _ := s.CreateAccount(Account{Name: "lain", PasswordHash: "h", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	s.CreateSession("h", acc.ID, now.Add(time.Hour))
	if got, err := s.SessionExpires("h"); err != nil || !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("expires: %v %v", got, err)
	}
	if _, err := s.SessionExpires("none"); err != ErrNotFound {
		t.Fatalf("unknown: %v", err)
	}
}

// A session vouched for by another (a QR login): only while that one is
// valid, in one statement.
func TestCreateSessionVouched(t *testing.T) {
	s := open(t)
	now := time.Now()
	acc, err := s.CreateAccount(Account{Name: "alice", PasswordHash: "x", CertPEM: []byte("c"), KeyPEM: []byte("k")}, now, "")
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSession("voucher", acc.ID, now.Add(time.Hour))
	if err := s.CreateSessionVouched("new", acc.ID, now.Add(time.Hour), "voucher", now); err != nil {
		t.Fatal(err)
	}
	if got, err := s.AccountBySession("new", now); err != nil || got.ID != acc.ID {
		t.Fatalf("vouched: %+v %v", got, err)
	}
	s.DeleteSession("voucher")
	if err := s.CreateSessionVouched("late", acc.ID, now.Add(time.Hour), "voucher", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("voucher gone: %v", err)
	}
	if _, err := s.AccountBySession("late", now); err == nil {
		t.Fatal("made without a voucher")
	}
}
