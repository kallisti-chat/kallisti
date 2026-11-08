package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Threads: a side conversation started from a message of a text channel.
// Its messages are in a hidden channel of its own (its id is the thread's),
// so history, reactions, pins, read marks and search work as in any chat.
// The thread stays when its start message goes.

// Thread is a thread, with its replies (messages) and the newest one's
// time (LastAt: the creation without replies).
type Thread struct {
	ID, Parent, Start int64
	Name              string
	Author            string
	AuthorID          *uint32 // a registered creator
	CreatedAt, LastAt time.Time
	Replies           int
	Archived          bool
	// The start message (StartAuthor empty: deleted since).
	StartAuthor, StartText string
}

var ErrThreadExists = errors.New("this message has a thread")

const threadsTable = `CREATE TABLE IF NOT EXISTS threads (
	channel_id INTEGER PRIMARY KEY REFERENCES channels(id),
	parent_id  INTEGER NOT NULL REFERENCES channels(id),
	start_id   INTEGER NOT NULL UNIQUE, -- no reference: the message may go
	name       TEXT NOT NULL,
	author     TEXT NOT NULL,
	author_id  INTEGER,
	created_at INTEGER NOT NULL,
	archived   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS threads_by_parent ON threads (parent_id, archived, channel_id);
CREATE TABLE IF NOT EXISTS thread_follows (
	user_id   INTEGER NOT NULL, -- the Mumble user id
	thread_id INTEGER NOT NULL REFERENCES threads(channel_id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, thread_id)
)`

// textOnly: a channels row is a text channel (not a voice channel's chat, a
// direct chat or a thread).
const textOnly = `voice_channel IS NULL AND dm_low IS NULL AND id NOT IN (SELECT channel_id FROM threads)`

// lastAt: the newest reply's time, or the creation.
const threadSelect = `SELECT t.channel_id, t.parent_id, t.start_id, t.name, t.author, t.author_id, t.created_at, t.archived,
		(SELECT count(*) FROM messages WHERE channel_id = t.channel_id),
		coalesce((SELECT created_at FROM messages WHERE channel_id = t.channel_id ORDER BY id DESC LIMIT 1), t.created_at),
		coalesce(s.author, ''), coalesce(s.text, '')
	FROM threads t LEFT JOIN messages s ON s.id = t.start_id `

func scanThread(row interface{ Scan(...any) error }) (Thread, error) {
	var t Thread
	var authorID sql.NullInt64
	var created, last int64
	err := row.Scan(&t.ID, &t.Parent, &t.Start, &t.Name, &t.Author, &authorID, &created, &t.Archived, &t.Replies, &last, &t.StartAuthor, &t.StartText)
	if errors.Is(err, sql.ErrNoRows) {
		return Thread{}, ErrNotFound
	}
	if authorID.Valid {
		id := uint32(authorID.Int64)
		t.AuthorID = &id
	}
	t.CreatedAt, t.LastAt = time.UnixMilli(created).UTC(), time.UnixMilli(last).UTC()
	return t, err
}

func (s *Store) threads(where string, args ...any) ([]Thread, error) {
	rows, err := s.db.Query(threadSelect+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateThread starts a thread from a message of a text channel (parent).
func (s *Store) CreateThread(parent, start int64, name, author string, authorID *uint32, at time.Time) (Thread, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Thread{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM messages WHERE id = ? AND channel_id = ?
		AND channel_id IN (SELECT id FROM channels WHERE `+textOnly+`)`, start, parent).Scan(&n); err != nil {
		return Thread{}, err
	}
	if n == 0 {
		return Thread{}, ErrNotFound
	}
	if err := tx.QueryRow(`SELECT count(*) FROM threads WHERE start_id = ?`, start).Scan(&n); err != nil || n > 0 {
		return Thread{}, cmpErr(err, ErrThreadExists)
	}
	// The name only has to be unique; text channel names cannot have ":".
	var id int64
	if err := tx.QueryRow(`INSERT INTO channels (name, position, hidden) VALUES (?, 0, 1) RETURNING id`, fmt.Sprintf("thread:%d", start)).Scan(&id); err != nil {
		return Thread{}, err
	}
	if _, err := tx.Exec(`INSERT INTO threads (channel_id, parent_id, start_id, name, author, author_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, parent, start, name, author, authorID, at.UnixMilli()); err != nil {
		return Thread{}, err
	}
	if err := tx.Commit(); err != nil {
		return Thread{}, err
	}
	return s.Thread(id)
}

// cmpErr: err if there is one, else alt.
func cmpErr(err, alt error) error {
	if err != nil {
		return err
	}
	return alt
}

func (s *Store) Thread(id int64) (Thread, error) {
	return scanThread(s.db.QueryRow(threadSelect+`WHERE t.channel_id = ?`, id))
}

// ThreadOf is the thread started from a message.
func (s *Store) ThreadOf(start int64) (Thread, error) {
	return scanThread(s.db.QueryRow(threadSelect+`WHERE t.start_id = ?`, start))
}

// Threads lists the active or archived threads of a text channel (0: of
// all), the newest first, before a thread (0: from the newest), at most
// limit (0: all).
func (s *Store) Threads(parent int64, archived bool, before int64, limit int) ([]Thread, error) {
	if limit <= 0 {
		limit = -1
	}
	return s.threads(`WHERE (? = 0 OR t.parent_id = ?) AND t.archived = ? AND (? = 0 OR t.channel_id < ?)
		ORDER BY t.channel_id DESC LIMIT ?`, parent, parent, archived, before, before, limit)
}

// ThreadParents: every thread's text channel, by thread.
func (s *Store) ThreadParents() (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT channel_id, parent_id FROM threads`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, parent int64
		if err := rows.Scan(&id, &parent); err != nil {
			return nil, err
		}
		out[id] = parent
	}
	return out, rows.Err()
}

func (s *Store) RenameThread(id int64, name string) error {
	return rowChanged(s.db.Exec(`UPDATE threads SET name = ? WHERE channel_id = ?`, name, id))
}

// SetThreadArchived archives a thread or opens it again; changed is false
// when it already was so.
func (s *Store) SetThreadArchived(id int64, on bool) (changed bool, err error) {
	var was bool
	if err := s.db.QueryRow(`SELECT archived FROM threads WHERE channel_id = ?`, id).Scan(&was); err != nil {
		return false, cmpErr(errNoRows(err), err)
	}
	if was == on {
		return false, nil
	}
	_, err = s.db.Exec(`UPDATE threads SET archived = ? WHERE channel_id = ?`, on, id)
	return err == nil, err
}

// errNoRows: ErrNotFound for no rows, else nil.
func errNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return nil
}

// ArchiveIdleThreads archives the active threads without a message since
// before (nor made since), and returns them.
func (s *Store) ArchiveIdleThreads(before time.Time) ([]int64, error) {
	return column[int64](s.db, `UPDATE threads SET archived = 1 WHERE archived = 0
		AND coalesce((SELECT created_at FROM messages WHERE channel_id = threads.channel_id ORDER BY id DESC LIMIT 1), created_at) < ?
		RETURNING channel_id`, before.UnixMilli())
}

// DeleteThread removes a thread with its messages, their attachments,
// follows and read marks, and returns the attachments' keys (the caller
// deletes the files).
func (s *Store) DeleteThread(id int64) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Select only file keys, not full message bodies or an unbounded list
	// of message ids. All related deletes use the same channel subquery.
	const messages = `SELECT id FROM messages WHERE channel_id = ?`
	keys, err := column[string](tx, `SELECT key FROM uploads WHERE message_id IN (`+messages+`) ORDER BY message_id, position`, id)
	if err != nil {
		return nil, err
	}
	for _, table := range []string{"uploads", "polls", "poll_votes"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE message_id IN (`+messages+`)`, id); err != nil {
			return nil, err
		}
	}
	// Reactions and search entries are removed by cascade/trigger.
	if _, err := tx.Exec(`DELETE FROM messages WHERE channel_id = ?`, id); err != nil {
		return nil, err
	}
	if err := rowChanged(tx.Exec(`DELETE FROM threads WHERE channel_id = ?`, id)); err != nil {
		return nil, err
	}
	// The hidden channel row stays (renamed): ids are never used twice,
	// as clients keep them.
	for _, q := range []string{`DELETE FROM read_marks_by_user WHERE channel_id = ?`, `UPDATE channels SET name = 'deleted-thread:' || id WHERE id = ?`} {
		if _, err := tx.Exec(q, id); err != nil {
			return nil, err
		}
	}
	return keys, tx.Commit()
}

// withExtras fills in messages' uploads, reactions and threads.
func (s *Store) withExtras(msgs []Message) error {
	if err := withUploads(s.db, msgs); err != nil {
		return err
	}
	if err := withPolls(s.db, msgs); err != nil {
		return err
	}
	if err := s.withReactions(msgs); err != nil {
		return err
	}
	return s.withThreads(msgs)
}

// withThreads fills in the threads started from messages.
func (s *Store) withThreads(msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	at := map[int64]int{}
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		at[m.ID], ids[i] = i, m.ID
	}
	ts, err := s.ThreadsFrom(ids)
	for _, t := range ts {
		msgs[at[t.Start]].Thread = &t
	}
	return err
}

// ThreadsFrom: the threads started from these messages.
func (s *Store) ThreadsFrom(starts []int64) ([]Thread, error) {
	if len(starts) == 0 {
		return nil, nil
	}
	in, args := inList(starts)
	return s.threads(`WHERE t.start_id IN (`+in+`)`, args...)
}

// Threads are archived after this long without a message (unless set).
const defaultThreadArchive = 3 * 24 * time.Hour

// ThreadArchiveAfter is how long threads stay active without a message.
func (s *Store) ThreadArchiveAfter() (time.Duration, error) {
	var seconds int64
	err := s.db.QueryRow(`SELECT value FROM store_meta WHERE key = 'thread_archive'`).Scan(&seconds)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultThreadArchive, nil
	}
	return time.Duration(seconds) * time.Second, err
}

func (s *Store) SetThreadArchiveAfter(d time.Duration) error {
	_, err := s.db.Exec(`INSERT INTO store_meta (key, value) VALUES ('thread_archive', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, int64(d/time.Second))
	return err
}

// Follow makes a registered user follow a thread, or not; changed is
// false when it already was so.
func (s *Store) Follow(user uint32, thread int64, on bool) (changed bool, err error) {
	q := `INSERT OR IGNORE INTO thread_follows (user_id, thread_id) VALUES (?, ?)`
	if !on {
		q = `DELETE FROM thread_follows WHERE user_id = ? AND thread_id = ?`
	}
	res, err := s.db.Exec(q, user, thread)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Followers of a thread, by user id.
func (s *Store) Followers(thread int64) ([]uint32, error) {
	return column[uint32](s.db, `SELECT user_id FROM thread_follows WHERE thread_id = ? ORDER BY user_id`, thread)
}

// Followed: the threads a user follows.
func (s *Store) Followed(user uint32) ([]int64, error) {
	return column[int64](s.db, `SELECT thread_id FROM thread_follows WHERE user_id = ? ORDER BY thread_id`, user)
}
