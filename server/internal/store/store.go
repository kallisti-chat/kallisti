// Package store keeps text channels and their messages in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	"kallisti.chat/server/internal/names"
)

type Channel struct {
	ID       int64
	Name     string
	Position int
	// Mumble is the channel that mirrors it for Mumble clients (0: none).
	Mumble uint32
	Topic  string
	// SlowMode: each user may post once per so many seconds (0: off).
	SlowMode int64
	Category int64 // 0: none
}

type Message struct {
	ID        int64
	ChannelID int64
	Author    string
	Text      string
	CreatedAt time.Time
	EditedAt  time.Time // zero: never edited
	PinnedAt  time.Time // zero: not pinned
	// The author was registered on the Mumble server (a verified name),
	// with this user id (nil: not known, from before ids were kept).
	Verified bool
	AuthorID *uint32
	Uploads  []Upload // attached files, in order
	// A sticker message (its Text: "[sticker: name]" for clients that
	// show text): the sticker, also once it was deleted.
	Sticker   *MessageSticker
	Reply     *Quote // the message it answers
	Reactions []Reaction
	// "everyone" or "here": the message mentions all (by a moderator).
	MentionAll string
	NoPing     bool    // a reply that does not ping the original's author
	Webhook    int64   // posted by this webhook (0: by a user; see Webhook)
	Thread     *Thread // the thread started from it
	Poll       *Poll   // a poll message (its Text: the poll as text)
}

// MessageSticker is the sticker a message is (its name now, also once
// deleted).
type MessageSticker struct {
	ID   int64
	Name string
}

// Quote is the message a reply answers (Author empty: deleted since).
type Quote struct {
	ID     int64
	Author string
	Text   string
	// A webhook's post: its name may be a user's (who is not answered).
	Webhook bool
}

// MaxUploads is the most files one message has; MaxDescription the
// longest description of one (characters).
const (
	MaxUploads     = 10
	MaxDescription = 1024
)

// Attachment is an upload to attach to a new message.
type Attachment struct {
	ID          int64
	Description string // alt text
}

// NewMessage is a message to store.
type NewMessage struct {
	ChannelID  int64
	Thread     bool // ChannelID is a thread's: refused once it is deleted
	Author     string
	Verified   bool
	AuthorID   uint32 // with Verified: the Mumble user id
	Text       string
	At         time.Time
	Uploads    []Attachment // unused uploads, in order (at most MaxUploads)
	StickerID  int64        // 0: not a sticker; else one that is not deleted
	ReplyTo    int64        // 0: not a reply; else a message in the same channel
	MentionAll string       // "", "here" or "everyone" (see Message)
	NoPing     bool         // see Message
	Poll       *Poll        // a poll (answers without voters)
	Webhook    int64        // see Message
	// The sender's id for this send ("": none), the same on each retry: a
	// second send with it finds the first (see MessageByNonce).
	Nonce string
}

// NonceTTL is how long a nonce finds its message.
const NonceTTL = 24 * time.Hour

// nonceAuthor is who a nonce is unique for: registered users by id (also
// after a rename), guests by name.
func nonceAuthor(n NewMessage) string {
	if n.Verified {
		return fmt.Sprintf("id:%d", n.AuthorID)
	}
	return "name:" + n.Author
}

// MessageByNonce is the author's message sent with n.Nonce in the day
// before n.At (ErrNotFound: none, or n has no nonce).
func (s *Store) MessageByNonce(n NewMessage) (Message, error) {
	if n.Nonce == "" {
		return Message{}, ErrNotFound
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM messages WHERE nonce_author = ? AND nonce = ? AND created_at >= ?`,
		nonceAuthor(n), n.Nonce, n.At.Add(-NonceTTL).UnixMilli()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	return s.MessageByID(id)
}

// Upload is a stored file. Key is the random, unguessable part of its URL.
type Upload struct {
	ID        int64
	Key       string
	Name      string
	Type      string
	Size      int64
	Author    string
	CreatedAt time.Time
	// Used: attached to a message. An upload can be attached once.
	Used bool
	Media
	// Preview: the type of a smaller picture made from it (see files),
	// "" if none was made yet, NoPreview if none can be.
	Preview     string
	Description string // set when it is attached (see Attachment)
}

const NoPreview = "-"

// HasPreview: a preview was made.
func (u Upload) HasPreview() bool {
	return strings.HasPrefix(u.Preview, "image/") || u.Preview == "video/mp4" // a GIF's
}

// Media: a picture's or video's size in pixels, a video's or audio's
// length in milliseconds (zero: unknown; see mediasize).
type Media struct {
	Width, Height int
	Duration      int64
}

type Store struct{ db *sql.DB }

// DB is the database, for other parts that keep their tables in the same
// file (the built-in Mumble server).
func (s *Store) DB() *sql.DB { return s.db }

const reactionsTable = `CREATE TABLE IF NOT EXISTS reactions (
	message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	emoji      TEXT NOT NULL,
	user_id    INTEGER NOT NULL, -- the Mumble user id
	user       TEXT NOT NULL,    -- the name to show
	created_at INTEGER NOT NULL,
	PRIMARY KEY (message_id, emoji, user_id)
)`

const schema = `
CREATE TABLE IF NOT EXISTS channels (
	id       INTEGER PRIMARY KEY,
	name     TEXT NOT NULL UNIQUE,
	position INTEGER NOT NULL,
	hidden   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS uploads (
	id         INTEGER PRIMARY KEY,
	key        TEXT NOT NULL UNIQUE,
	name       TEXT NOT NULL,
	type       TEXT NOT NULL,
	size       INTEGER NOT NULL,
	author     TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	used       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY,
	channel_id INTEGER NOT NULL REFERENCES channels(id),
	author     TEXT NOT NULL,
	text       TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	upload_id  INTEGER REFERENCES uploads(id),
	verified   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS messages_by_channel ON messages(channel_id, id);
-- Counters; last_message_id: the highest message id ever given.
CREATE TABLE IF NOT EXISTS store_meta (
	key   TEXT PRIMARY KEY,
	value INTEGER NOT NULL
);
-- Random keys of the server's own (see Secret).
CREATE TABLE IF NOT EXISTS secrets (
	name  TEXT PRIMARY KEY,
	value BLOB NOT NULL
);
-- The newest message a registered user (Mumble user id) read in a channel.
CREATE TABLE IF NOT EXISTS read_marks_by_user (
	user_id    INTEGER NOT NULL,
	channel_id INTEGER NOT NULL,
	message_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, channel_id)
);

CREATE TABLE IF NOT EXISTS accounts (
	id            INTEGER PRIMARY KEY,
	name          TEXT NOT NULL,
	name_key      TEXT NOT NULL UNIQUE, -- see names.Key
	password_hash TEXT NOT NULL,
	cert_pem      BLOB NOT NULL,
	key_pem       BLOB NOT NULL,
	created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);
`

// Open opens (and creates) the database. ":memory:" is for tests.
func Open(path string) (*Store, error) {
	// synchronous=NORMAL: with WAL still safe against corruption, and an
	// insert does not wait for a full disk sync (the sender's voice waits
	// behind it on the same connection).
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One writer at a time is all SQLite allows anyway, and an in-memory
	// database exists only for one connection.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema + reactionsTable + ";" + statusTable + ";" + channelListTable + ";" + emojiTable + ";" + userEmojiTables + ";" + stickersTable + ";" + invitesTable + ";" + webhooksTable + ";" + pushTables + ";" + streamKeysTable + ";" + categoriesTable + ";" + threadsTable + ";" + fediTables + ";" + pollsTables + ";" + logoTable + ";" + accountServersTable + ";" + friendsTable + ";" + oauthTables + ";" + registrationTables); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// addColumns adds the columns (name, definition) a table does not have yet.
func addColumns(db *sql.DB, table string, cols [][2]string) error {
	for _, col := range cols {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, col[0]).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + col[0] + ` ` + col[1]); err != nil {
				return err
			}
		}
	}
	return nil
}

// byUserID moves data kept by name to user ids (see the built-in server's
// mumble_users, in the same database): verified authors and read marks.
// Verified authors without a registration lose the mark (their names are
// free now). With an external Murmur there are no ids to find: authors keep
// their names (see sidecar's wrote) and the old read marks go.
func byUserID(db *sql.DB) error {
	var mumbleUsers, oldMarks int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'mumble_users'`).Scan(&mumbleUsers); err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'read_marks'`).Scan(&oldMarks); err != nil {
		return err
	}
	ids, err := userIDs(db, mumbleUsers > 0)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if mumbleUsers > 0 {
		authors, err := column[string](tx, `SELECT DISTINCT author FROM messages WHERE verified = 1 AND author_id IS NULL`)
		if err != nil {
			return err
		}
		for _, a := range authors {
			q, args := `UPDATE messages SET verified = 0 WHERE verified = 1 AND author_id IS NULL AND author = ?`, []any{a}
			if id, ok := ids[names.Key(a)]; ok {
				q, args = `UPDATE messages SET author_id = ? WHERE verified = 1 AND author_id IS NULL AND author = ?`, []any{id, a}
			}
			if _, err := tx.Exec(q, args...); err != nil {
				return err
			}
		}
	}
	if oldMarks > 0 {
		users, err := column[string](tx, `SELECT DISTINCT user FROM read_marks`)
		if err != nil {
			return err
		}
		for _, u := range users {
			if id, ok := ids[u]; ok {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO read_marks_by_user (user_id, channel_id, message_id)
					SELECT ?, channel_id, message_id FROM read_marks WHERE user = ?`, id, u); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(`DROP TABLE read_marks`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// userIDs are the built-in server's registrations by names.Key (none
// without its table).
func userIDs(db *sql.DB, exists bool) (map[string]uint32, error) {
	ids := map[string]uint32{}
	if !exists {
		return ids, nil
	}
	rows, err := db.Query(`SELECT name_key, id FROM mumble_users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var id uint32
		if err := rows.Scan(&key, &id); err != nil {
			return nil, err
		}
		ids[key] = id
	}
	return ids, rows.Err()
}

// column reads one column of a query.
func column[T any](db interface {
	Query(string, ...any) (*sql.Rows, error)
}, query string, args ...any) ([]T, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var v T
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// reactionsByID rebuilds a reactions table of the first version (kept by
// name): registered users of the built-in server (mumble_users, in the same
// database) keep theirs, the rest go.
func reactionsByID(db *sql.DB) error {
	var byName, mumbleUsers int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('reactions') WHERE name = 'user_key'`).Scan(&byName); err != nil || byName == 0 {
		return err
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'mumble_users'`).Scan(&mumbleUsers); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := []string{`ALTER TABLE reactions RENAME TO reactions_by_name`, reactionsTable}
	if mumbleUsers > 0 {
		queries = append(queries, `INSERT OR IGNORE INTO reactions (message_id, emoji, user_id, user, created_at)
			SELECT r.message_id, r.emoji, u.id, r.user, r.created_at FROM reactions_by_name r JOIN mumble_users u ON u.name_key = r.user_key`)
	}
	for _, q := range append(queries, `DROP TABLE reactions_by_name`) {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// uploadMessages: before several files per message, a message kept its one
// upload in messages.upload_id (no longer written); the uploads now name
// their message. Once (upload_messages in store_meta).
func uploadMessages(db *sql.DB) error {
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS uploads_by_message ON uploads(message_id, position) WHERE message_id IS NOT NULL`); err != nil {
		return err
	}
	var done int
	if err := db.QueryRow(`SELECT count(*) FROM store_meta WHERE key = 'upload_messages'`).Scan(&done); err != nil || done > 0 {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`UPDATE uploads SET message_id = m.id FROM messages m WHERE m.upload_id = uploads.id AND uploads.message_id IS NULL`,
		// Its foreign key would keep an upload removed from its message.
		`UPDATE messages SET upload_id = NULL WHERE upload_id IS NOT NULL`,
		`INSERT INTO store_meta (key, value) VALUES ('upload_messages', 1)`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrate brings databases from older versions up to the current schema:
// columns added later are added if missing.
func migrate(db *sql.DB) error {
	if err := addColumns(db, "messages", [][2]string{
		{"upload_id", "INTEGER REFERENCES uploads(id)"}, // no longer written (see uploadMessages)
		{"verified", "INTEGER NOT NULL DEFAULT 0"},
		{"edited_at", "INTEGER"},
		{"reply_to", "INTEGER"},
		{"author_id", "INTEGER"}, // the Mumble user id of verified authors
		{"pinned_at", "INTEGER"},
		{"mention_all", "TEXT NOT NULL DEFAULT ''"},
		{"no_ping", "INTEGER NOT NULL DEFAULT 0"},
		{"webhook_id", "INTEGER NOT NULL DEFAULT 0"}, // see Message.Webhook
		{"nonce", "TEXT"},                            // the sender's id for the send (see MessageByNonce)
		{"nonce_author", "TEXT"},                     // see nonceAuthor
		{"sticker_id", "INTEGER"},
	}); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS messages_nonce ON messages (nonce_author, nonce) WHERE nonce IS NOT NULL`); err != nil {
		return err
	}
	if err := byUserID(db); err != nil {
		return err
	}
	if err := addColumns(db, "uploads", [][2]string{
		{"width", "INTEGER NOT NULL DEFAULT 0"},
		{"height", "INTEGER NOT NULL DEFAULT 0"},
		{"duration", "INTEGER NOT NULL DEFAULT 0"},
		{"preview", "TEXT NOT NULL DEFAULT ''"},
		{"message_id", "INTEGER"}, // the message it is attached to
		{"position", "INTEGER NOT NULL DEFAULT 0"},
		{"description", "TEXT NOT NULL DEFAULT ''"},
	}); err != nil {
		return err
	}
	if err := uploadMessages(db); err != nil {
		return err
	}
	if err := addColumns(db, "accounts", [][2]string{
		{"temporary", "INTEGER NOT NULL DEFAULT 0"},
		{"last_seen", "INTEGER"}, // Unix milliseconds
	}); err != nil {
		return err
	}
	if err := reactionsByID(db); err != nil {
		return err
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('channels') WHERE name = 'voice_channel'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// The Mumble channel whose chat this hidden channel is.
		if _, err := db.Exec(`ALTER TABLE channels ADD COLUMN voice_channel INTEGER`); err != nil {
			return err
		}
		if _, err := db.Exec(`CREATE UNIQUE INDEX channels_voice ON channels (voice_channel) WHERE voice_channel IS NOT NULL`); err != nil {
			return err
		}
	}
	// Direct chats: the two users (see direct.go).
	if err := addColumns(db, "channels", [][2]string{{"dm_low", "INTEGER"}, {"dm_high", "INTEGER"}}); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS channels_direct ON channels (dm_low, dm_high) WHERE dm_low IS NOT NULL`); err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'messages_fts'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// Full-text search over message texts, kept current by triggers;
		// filled once from the messages there are.
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, q := range []string{
			`CREATE VIRTUAL TABLE messages_fts USING fts5(text, content='messages', content_rowid='id',
				tokenize='unicode61 remove_diacritics 2', prefix='2 3')`,
			`CREATE TRIGGER messages_fts_insert AFTER INSERT ON messages BEGIN
				INSERT INTO messages_fts (rowid, text) VALUES (new.id, new.text); END`,
			`CREATE TRIGGER messages_fts_delete AFTER DELETE ON messages BEGIN
				INSERT INTO messages_fts (messages_fts, rowid, text) VALUES ('delete', old.id, old.text); END`,
			`CREATE TRIGGER messages_fts_update AFTER UPDATE OF text ON messages BEGIN
				INSERT INTO messages_fts (messages_fts, rowid, text) VALUES ('delete', old.id, old.text);
				INSERT INTO messages_fts (rowid, text) VALUES (new.id, new.text); END`,
			`INSERT INTO messages_fts (messages_fts) VALUES ('rebuild')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("search index: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("search index: %w", err)
		}
	}
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('channels') WHERE name = 'topic'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE channels ADD COLUMN topic TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('channels') WHERE name = 'mumble_channel'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// The Mumble channel that mirrors a text channel.
		if _, err := db.Exec(`ALTER TABLE channels ADD COLUMN mumble_channel INTEGER`); err != nil {
			return err
		}
	}
	if err := addColumns(db, "channels", [][2]string{{"slow_mode", "INTEGER NOT NULL DEFAULT 0"}, {"category_id", "INTEGER"}}); err != nil {
		return err
	}
	// Message and account ids counted from before the counters: the newest
	// there are.
	_, err := db.Exec(`INSERT OR IGNORE INTO store_meta (key, value)
		SELECT 'last_message_id', coalesce(max(id), 0) FROM messages;
		INSERT OR IGNORE INTO store_meta (key, value)
		SELECT 'last_account_id', coalesce(max(id), 0) FROM accounts`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

// VoiceChat is the hidden channel that holds a voice channel's chat (the
// Mumble channel id; the built-in server never reuses them), made on first
// use.
func (s *Store) VoiceChat(voice uint32) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM channels WHERE voice_channel = ?`, voice).Scan(&id)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return id, err
	}
	// The name only has to be unique; text channel names cannot have ":".
	err = s.db.QueryRow(`INSERT INTO channels (name, position, hidden, voice_channel) VALUES (?, 0, 1, ?)
		ON CONFLICT (voice_channel) WHERE voice_channel IS NOT NULL DO UPDATE SET hidden = 1 RETURNING id`,
		fmt.Sprintf("voice:%d", voice), voice).Scan(&id)
	return id, err
}

// CountAfter counts a channel's messages after one.
func (s *Store) CountAfter(channelID, afterID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM messages WHERE channel_id = ? AND id > ?`, channelID, afterID).Scan(&n)
	return n, err
}

// FindVoiceChat is a voice channel's chat if it has one (VoiceChat makes
// one).
func (s *Store) FindVoiceChat(voice uint32) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM channels WHERE voice_channel = ?`, voice).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// VoiceChats are the voice channel chats: store channel -> voice channel.
func (s *Store) VoiceChats() (map[int64]uint32, error) {
	rows, err := s.db.Query(`SELECT id, voice_channel FROM channels WHERE voice_channel IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]uint32{}
	for rows.Next() {
		var id int64
		var voice uint32
		if err := rows.Scan(&id, &voice); err != nil {
			return nil, err
		}
		out[id] = voice
	}
	return out, rows.Err()
}

// VoiceOf is the voice channel whose chat a channel is (ok false for text
// channels).
func (s *Store) VoiceOf(channel int64) (voice uint32, ok bool, err error) {
	var v sql.NullInt64
	err = s.db.QueryRow(`SELECT voice_channel FROM channels WHERE id = ?`, channel).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return uint32(v.Int64), v.Valid, err
}

// EnsureChannels makes the configured channels visible, in this order.
// Channels that are no longer configured are hidden, not deleted, so their
// history comes back if they are configured again.
func (s *Store) EnsureChannels(names []string) error {
	for _, n := range names {
		if strings.Contains(n, ":") {
			return ErrInvalidName
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE channels SET hidden = 1 WHERE ` + textOnly); err != nil {
		return err
	}
	for i, name := range names {
		_, err := tx.Exec(`INSERT INTO channels (name, position) VALUES (?, ?)
			ON CONFLICT (name) DO UPDATE SET position = excluded.position, hidden = 0`, name, i)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(dropHiddenWebhooks); err != nil {
		return err
	}
	return tx.Commit()
}

// dropHiddenWebhooks: a hidden channel's webhooks go (one that comes back
// has no old secret URLs).
const dropHiddenWebhooks = `DELETE FROM webhooks WHERE channel IN (SELECT id FROM channels WHERE hidden = 1)`

// SeedChannels makes the first text channels of a new server (later they
// are managed in the client).
func (s *Store) SeedChannels(names []string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM channels WHERE ` + textOnly).Scan(&n); err != nil || n > 0 {
		return err
	}
	return s.EnsureChannels(names)
}

// CreateChannel adds a text channel at the end, in a category (0: none).
// With revive, a deleted channel of that name comes back, with its history.
func (s *Store) CreateChannel(name string, category int64, revive bool) (Channel, error) {
	if strings.Contains(name, ":") {
		return Channel{}, ErrInvalidName
	}
	if err := s.categoryExists(category); err != nil {
		return Channel{}, err
	}
	c := Channel{Name: name, Category: category}
	err := s.db.QueryRow(`INSERT INTO channels (name, position, category_id)
		VALUES (?, (SELECT COALESCE(MAX(position), -1) + 1 FROM channels WHERE hidden = 0), NULLIF(?, 0))
		ON CONFLICT (name) DO UPDATE SET hidden = 0, position = excluded.position, category_id = excluded.category_id
		WHERE hidden = 1 AND `+textOnly+` AND ?
		RETURNING id, position`, name, category, revive).Scan(&c.ID, &c.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, ErrNameTaken // a visible channel has the name (or a deleted one)
	}
	return c, err
}

// textChannel runs a change on a text channel (not a voice channel chat).
func (s *Store) textChannel(query string, args ...any) error {
	return rowChanged(s.db.Exec(query, args...))
}

func (s *Store) RenameChannel(id int64, name string) error {
	if strings.Contains(name, ":") {
		return ErrInvalidName
	}
	return s.textChannel(`UPDATE channels SET name = ? WHERE id = ? AND `+textOnly, name, id)
}

// HideChannel removes a text channel from the list; its history stays,
// its webhooks go (see dropHiddenWebhooks).
func (s *Store) HideChannel(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := rowChanged(tx.Exec(`UPDATE channels SET hidden = 1 WHERE id = ? AND `+textOnly, id)); err != nil {
		return err
	}
	if _, err := tx.Exec(dropHiddenWebhooks); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPositions puts the given text channels in this order.
func (s *Store) SetPositions(ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE channels SET position = ? WHERE id = ? AND `+textOnly, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetTopic sets a text channel's topic.
func (s *Store) SetTopic(id int64, topic string) error {
	return s.textChannel(`UPDATE channels SET topic = ? WHERE id = ? AND `+textOnly, topic, id)
}

// SetSlowMode sets a text channel's slow mode (seconds, 0: off).
func (s *Store) SetSlowMode(id, seconds int64) error {
	return s.textChannel(`UPDATE channels SET slow_mode = ? WHERE id = ? AND `+textOnly, seconds, id)
}

// SetMumbleChannel remembers the Mumble channel that mirrors a text channel.
func (s *Store) SetMumbleChannel(id int64, mumble uint32) error {
	_, err := s.db.Exec(`UPDATE channels SET mumble_channel = ? WHERE id = ? AND `+textOnly, mumble, id)
	return err
}

// Channels returns the visible channels in order.
func (s *Store) Channels() ([]Channel, error) {
	rows, err := s.db.Query(`SELECT id, name, position, COALESCE(mumble_channel, 0), topic, slow_mode, COALESCE(category_id, 0) FROM channels WHERE hidden = 0 ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.Position, &c.Mumble, &c.Topic, &c.SlowMode, &c.Category); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddMessage stores a message. Its uploads are marked as used in the same
// transaction; a used upload is refused.
func (s *Store) AddMessage(n NewMessage) (Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()
	// A deleted thread keeps its channel row: the thread itself is checked
	// here, as it may be gone since the caller found it.
	// A webhook deleted (or its channel hidden) meanwhile posts nothing.
	if n.Webhook != 0 {
		var live int
		if err := tx.QueryRow(`SELECT count(*) FROM webhooks WHERE id = ? AND channel = ?`, n.Webhook, n.ChannelID).Scan(&live); err != nil || live == 0 {
			return Message{}, cmpErr(err, ErrNotFound)
		}
	}
	if n.Thread {
		var live int
		if err := tx.QueryRow(`SELECT count(*) FROM threads WHERE channel_id = ?`, n.ChannelID).Scan(&live); err != nil || live == 0 {
			return Message{}, cmpErr(err, ErrNotFound)
		}
	}
	if len(n.Uploads) > MaxUploads {
		return Message{}, errors.New("too many uploads")
	}
	var sticker sql.NullInt64
	var ms *MessageSticker
	if n.StickerID != 0 {
		st := MessageSticker{ID: n.StickerID}
		err := tx.QueryRow(`SELECT name FROM stickers WHERE id = ? AND data IS NOT NULL`, n.StickerID).Scan(&st.Name)
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, ErrNotFound
		} else if err != nil {
			return Message{}, err
		}
		sticker, ms = sql.NullInt64{Int64: n.StickerID, Valid: true}, &st
	}
	var reply sql.NullInt64
	var quote *Quote
	if n.ReplyTo != 0 {
		q := Quote{ID: n.ReplyTo}
		err := tx.QueryRow(`SELECT author, text, webhook_id != 0 FROM messages WHERE id = ? AND channel_id = ?`, n.ReplyTo, n.ChannelID).Scan(&q.Author, &q.Text, &q.Webhook)
		if err != nil {
			return Message{}, errors.New("the message to answer is not in this channel")
		}
		reply, quote = sql.NullInt64{Int64: n.ReplyTo, Valid: true}, &q
	}
	var authorID *uint32
	if n.Verified {
		authorID = &n.AuthorID
	}
	// Ids are never used twice (read marks only go up), so the highest id
	// ever given is kept in store_meta.
	var id int64
	err = tx.QueryRow(`UPDATE store_meta SET value = max(value, (SELECT coalesce(max(id), 0) FROM messages)) + 1
		WHERE key = 'last_message_id' RETURNING value`).Scan(&id)
	if err != nil {
		return Message{}, err
	}
	var nonce, nonceBy sql.NullString
	if n.Nonce != "" {
		nonce, nonceBy = sql.NullString{String: n.Nonce, Valid: true}, sql.NullString{String: nonceAuthor(n), Valid: true}
		// An expired use of the nonce no longer counts.
		if _, err := tx.Exec(`UPDATE messages SET nonce = NULL, nonce_author = NULL WHERE nonce_author = ? AND nonce = ? AND created_at < ?`,
			nonceBy, nonce, n.At.Add(-NonceTTL).UnixMilli()); err != nil {
			return Message{}, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO messages (id, channel_id, author, verified, author_id, text, created_at, sticker_id, reply_to, mention_all, no_ping, nonce, nonce_author, webhook_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, n.ChannelID, n.Author, n.Verified, authorID, n.Text, n.At.UnixMilli(), sticker, reply, n.MentionAll, n.NoPing, nonce, nonceBy, n.Webhook); err != nil {
		return Message{}, err
	}
	// An upload attached once is used: the same one twice fails too.
	for i, a := range n.Uploads {
		res, err := tx.Exec(`UPDATE uploads SET used = 1, message_id = ?, position = ?, description = ? WHERE id = ? AND used = 0`,
			id, i, a.Description, a.ID)
		if err != nil {
			return Message{}, err
		}
		if rows, _ := res.RowsAffected(); rows != 1 {
			return Message{}, errors.New("upload is missing or already attached")
		}
	}
	if n.Poll != nil {
		if err := addPoll(tx, id, *n.Poll); err != nil {
			return Message{}, err
		}
	}
	msgs := []Message{{ID: id, ChannelID: n.ChannelID, Author: n.Author, Verified: n.Verified, AuthorID: authorID, Text: n.Text, CreatedAt: n.At, Sticker: ms, Reply: quote, MentionAll: n.MentionAll, NoPing: n.NoPing, Webhook: n.Webhook}}
	if err := withUploads(tx, msgs); err != nil {
		return Message{}, err
	}
	if n.Poll != nil {
		if err := withPolls(tx, msgs); err != nil {
			return Message{}, err
		}
	}
	return msgs[0], tx.Commit()
}

func (s *Store) AddUpload(u Upload, at time.Time) (Upload, error) {
	res, err := s.db.Exec(`INSERT INTO uploads (key, name, type, size, author, created_at, width, height, duration) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Key, u.Name, u.Type, u.Size, u.Author, at.UnixMilli(), u.Width, u.Height, u.Duration)
	if err != nil {
		return Upload{}, err
	}
	u.ID, err = res.LastInsertId()
	u.CreatedAt = at
	return u, err
}

// MeasureUploads gives the uploads from before sizes were kept their
// Media, once (upload_sizes in store_meta). The files are read with no
// connection held; the results are stored in one transaction.
func (s *Store) MeasureUploads(measure func(Upload) Media) error {
	var done int
	if err := s.db.QueryRow(`SELECT count(*) FROM store_meta WHERE key = 'upload_sizes'`).Scan(&done); err != nil || done > 0 {
		return err
	}
	rows, err := s.db.Query(`SELECT ` + uploadColumns + ` FROM uploads ORDER BY id`)
	if err != nil {
		return err
	}
	var all []Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			rows.Close()
			return err
		}
		all = append(all, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	media := make([]Media, len(all))
	for i, u := range all {
		media[i] = measure(u)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, m := range media {
		if m == (Media{}) {
			continue
		}
		if _, err := tx.Exec(`UPDATE uploads SET width = ?, height = ?, duration = ? WHERE id = ?`, m.Width, m.Height, m.Duration, all[i].ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO store_meta (key, value) VALUES ('upload_sizes', 1)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UploadByKey(key string) (Upload, error) {
	u, err := scanUpload(s.db.QueryRow(`SELECT `+uploadColumns+` FROM uploads WHERE key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Upload{}, ErrNotFound
	}
	return u, err
}

// SetUploadPreview keeps an upload's preview type (see Upload.Preview);
// found is false if the upload is gone.
func (s *Store) SetUploadPreview(key, ctype string) (found bool, err error) {
	res, err := s.db.Exec(`UPDATE uploads SET preview = ? WHERE key = ?`, ctype, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UploadsWithoutPreview: attached pictures and videos without a preview
// (made or refused), the newest first.
func (s *Store) UploadsWithoutPreview() ([]Upload, error) {
	rows, err := s.db.Query(`SELECT ` + uploadColumns + ` FROM uploads
		WHERE used = 1 AND preview = '' AND (type LIKE 'image/%' OR type LIKE 'video/%') ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// MessageByUpload is the message an upload is attached to.
func (s *Store) MessageByUpload(key string) (Message, error) {
	var id int64
	err := s.db.QueryRow(`SELECT message_id FROM uploads WHERE key = ? AND message_id IS NOT NULL`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	return s.MessageByID(id)
}

// UploadChannel is the channel of the message an upload is attached to.
func (s *Store) UploadChannel(key string) (int64, error) {
	var channel int64
	err := s.db.QueryRow(`SELECT m.channel_id FROM uploads u JOIN messages m ON m.id = u.message_id WHERE u.key = ?`, key).Scan(&channel)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return channel, err
}

const uploadColumns = `id, key, name, type, size, author, created_at, used, width, height, duration, preview, description`

func scanUpload(row interface{ Scan(...any) error }, more ...any) (Upload, error) {
	var u Upload
	var at int64
	err := row.Scan(append([]any{&u.ID, &u.Key, &u.Name, &u.Type, &u.Size, &u.Author, &at, &u.Used, &u.Width, &u.Height, &u.Duration, &u.Preview, &u.Description}, more...)...)
	u.CreatedAt = time.UnixMilli(at).UTC()
	return u, err
}

// withUploads fills in messages' uploads.
func withUploads(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	at := map[int64]int{}
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		at[m.ID], ids[i] = i, m.ID
	}
	in, args := inList(ids)
	rows, err := q.Query(`SELECT `+uploadColumns+`, message_id FROM uploads WHERE message_id IN (`+in+`) ORDER BY message_id, position`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		u, err := scanUpload(rows, &id)
		if err != nil {
			return err
		}
		m := &msgs[at[id]]
		m.Uploads = append(m.Uploads, u)
	}
	return rows.Err()
}

const messageSelect = `SELECT m.id, m.channel_id, m.author, m.verified, m.author_id, m.text, m.created_at, m.edited_at, m.pinned_at,
		m.reply_to, r.author, r.text, COALESCE(r.webhook_id, 0) != 0, m.mention_all, m.no_ping, m.sticker_id, st.name, m.webhook_id
	FROM messages m LEFT JOIN messages r ON r.id = m.reply_to LEFT JOIN stickers st ON st.id = m.sticker_id `

func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var at int64
	var edited, pinned sql.NullInt64
	var replyTo, authorID sql.NullInt64
	var replyAuthor, replyText, stickerName sql.NullString
	var replyWebhook bool
	var stickerID sql.NullInt64
	if err := row.Scan(&m.ID, &m.ChannelID, &m.Author, &m.Verified, &authorID, &m.Text, &at, &edited, &pinned,
		&replyTo, &replyAuthor, &replyText, &replyWebhook, &m.MentionAll, &m.NoPing, &stickerID, &stickerName, &m.Webhook); err != nil {
		return Message{}, err
	}
	if authorID.Valid {
		id := uint32(authorID.Int64)
		m.AuthorID = &id
	}
	if replyTo.Valid {
		m.Reply = &Quote{ID: replyTo.Int64, Author: replyAuthor.String, Text: replyText.String, Webhook: replyWebhook}
	}
	if stickerID.Valid {
		m.Sticker = &MessageSticker{ID: stickerID.Int64, Name: stickerName.String}
	}
	m.CreatedAt = time.UnixMilli(at).UTC()
	if edited.Valid {
		m.EditedAt = time.UnixMilli(edited.Int64).UTC()
	}
	if pinned.Valid {
		m.PinnedAt = time.UnixMilli(pinned.Int64).UTC()
	}
	return m, nil
}

// History returns up to limit messages before beforeID (0: the newest),
// oldest first, with their attachments.
func (s *Store) History(channelID, beforeID int64, limit int) ([]Message, error) {
	rows, err := s.db.Query(messageSelect+`WHERE m.channel_id = ? AND (? = 0 OR m.id < ?) ORDER BY m.id DESC LIMIT ?`,
		channelID, beforeID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	slices.Reverse(out)
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.withExtras(out)
}

func (s *Store) MessageByID(id int64) (Message, error) {
	m, err := scanMessage(s.db.QueryRow(messageSelect+`WHERE m.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	msgs := []Message{m}
	if err := s.withExtras(msgs); err != nil {
		return Message{}, err
	}
	return msgs[0], nil
}

// EditMessage replaces a message's text (and its MentionAll), takes the
// uploads `remove` off it (others are left alone; the caller deletes the
// files), and returns it.
func (s *Store) EditMessage(id int64, text, mentionAll string, at time.Time, remove ...int64) (Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE messages SET text = ?, edited_at = ?, mention_all = ? WHERE id = ?`, text, at.UnixMilli(), mentionAll, id)
	if err != nil {
		return Message{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Message{}, ErrNotFound
	}
	if len(remove) > 0 {
		in, args := inList(remove)
		if _, err := tx.Exec(`DELETE FROM uploads WHERE message_id = ? AND id IN (`+in+`)`, append([]any{id}, args...)...); err != nil {
			return Message{}, err
		}
	}
	// Checked here as well: edits from two connections may cross.
	var left int
	if err := tx.QueryRow(`SELECT count(*) FROM uploads WHERE message_id = ?`, id).Scan(&left); err != nil {
		return Message{}, err
	}
	if text == "" && left == 0 {
		return Message{}, ErrEmpty
	}
	if err := tx.Commit(); err != nil {
		return Message{}, err
	}
	return s.MessageByID(id)
}

// DeleteMessage removes a message and its attachment, and returns it (the
// caller deletes the attachment's file).
func (s *Store) DeleteMessage(id int64) (Message, error) {
	gone, err := s.DeleteMessages([]int64{id})
	if err == nil && len(gone) == 0 {
		err = ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	return gone[0], nil
}

// DeleteMessages removes those of the messages that exist, with their
// attachments, and returns them by id (without reactions; the caller
// deletes the attachments' files).
func (s *Store) DeleteMessages(ids []int64) ([]Message, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	gone, err := deleteMessages(tx, ids)
	if err != nil {
		return nil, err
	}
	return gone, tx.Commit()
}

func deleteMessages(tx *sql.Tx, ids []int64) ([]Message, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in, args := inList(ids)
	rows, err := tx.Query(messageSelect+`WHERE m.id IN (`+in+`) ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	var gone []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		gone = append(gone, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := withUploads(tx, gone); err != nil {
		return nil, err
	}
	for _, q := range []string{`DELETE FROM messages WHERE id IN (` + in + `)`, `DELETE FROM uploads WHERE message_id IN (` + in + `)`,
		`DELETE FROM polls WHERE message_id IN (` + in + `)`, `DELETE FROM poll_votes WHERE message_id IN (` + in + `)`} {
		if _, err := tx.Exec(q, args...); err != nil {
			return nil, err
		}
	}
	return gone, nil
}

// RecentByUser: the newest messages (at most limit) of a registered user
// since a time, outside direct chats; RecentAmong: those of these.
func (s *Store) RecentByUser(user uint32, since time.Time, limit int) ([]int64, error) {
	return s.recent(`m.verified = 1 AND m.author_id = ?`, []any{user}, since, limit)
}

func (s *Store) RecentAmong(ids []int64, since time.Time, limit int) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in, args := inList(ids)
	return s.recent(`m.id IN (`+in+`)`, args, since, limit)
}

func (s *Store) recent(where string, args []any, since time.Time, limit int) ([]int64, error) {
	rows, err := s.db.Query(`SELECT m.id FROM messages m JOIN channels c ON c.id = m.channel_id
		WHERE `+where+` AND m.created_at >= ? AND c.dm_low IS NULL ORDER BY m.id DESC LIMIT ?`,
		append(args, since.UnixMilli(), limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// inList is "?,?,…" and the arguments for a list of values.
func inList[T any](values []T) (string, []any) {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(values)), ","), args
}

// At most this many words of a search are used.
const maxSearchTerms = 10

// ftsQuery makes search text an FTS5 query: every word must appear, as a
// prefix (one letter: exactly); each word is quoted, so FTS syntax in it
// is only text. Words without letters or digits are left out.
func ftsQuery(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1 // FTS5 refuses NUL in a string
		}
		return r
	}, text)
	var terms []string
	for _, w := range strings.Fields(text) {
		if !strings.ContainsFunc(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			continue
		}
		term := `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
		// One letter as a prefix matches nearly everything (and slowly).
		if utf8.RuneCountInString(w) > 1 {
			term += "*"
		}
		terms = append(terms, term)
		if len(terms) == maxSearchTerms {
			break
		}
	}
	return strings.Join(terms, " ")
}

// HasSearchWords tells if text has words to search for (not only
// punctuation).
func HasSearchWords(text string) bool { return ftsQuery(text) != "" }

// Search finds messages in the given channels whose text has all words of
// text (as prefixes; no words: all), optionally by one author (any case);
// newest first.
func (s *Store) Search(text string, channels []int64, author string, limit int) ([]Message, error) {
	if len(channels) == 0 {
		return nil, nil
	}
	// Two forms: with an OR, SQLite would scan the table, not the index.
	where, args := `1`, []any{}
	if q := ftsQuery(text); q != "" {
		where, args = `m.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)`, append(args, q)
	}
	in, chans := inList(channels)
	args = append(append(args, chans...), author, author, limit)
	rows, err := s.db.Query(messageSelect+`WHERE `+where+`
		AND m.channel_id IN (`+in+`)
		AND (? = '' OR lower(m.author) = lower(?)) ORDER BY m.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := withUploads(s.db, out); err != nil {
		return nil, err
	}
	return out, withPolls(s.db, out)
}

// SetReadMark notes that a user read a channel up to a message; marks never
// go back.
func (s *Store) SetReadMark(userID uint32, channel, message int64) error {
	_, err := s.db.Exec(`INSERT INTO read_marks_by_user (user_id, channel_id, message_id) VALUES (?, ?, ?)
		ON CONFLICT (user_id, channel_id) DO UPDATE SET message_id = max(message_id, excluded.message_id)`, userID, channel, message)
	return err
}

// ResetReadMark sets a user's read mark in a channel, also back (mark
// unread).
func (s *Store) ResetReadMark(userID uint32, channel, message int64) error {
	_, err := s.db.Exec(`INSERT INTO read_marks_by_user (user_id, channel_id, message_id) VALUES (?, ?, ?)
		ON CONFLICT (user_id, channel_id) DO UPDATE SET message_id = excluded.message_id`, userID, channel, message)
	return err
}

// ReadMark is a user's read mark in one channel (ok false: none).
func (s *Store) ReadMark(userID uint32, channel int64) (message int64, ok bool, err error) {
	err = s.db.QueryRow(`SELECT message_id FROM read_marks_by_user WHERE user_id = ? AND channel_id = ?`, userID, channel).Scan(&message)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return message, err == nil, err
}

// ReadMarks are a user's read marks by channel.
func (s *Store) ReadMarks(userID uint32) (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT channel_id, message_id FROM read_marks_by_user WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var c, m int64
		if err := rows.Scan(&c, &m); err != nil {
			return nil, err
		}
		out[c] = m
	}
	return out, rows.Err()
}

// At most this many messages are looked at for MessagesAfter.
const maxMessagesAfter = 500

// MessagesAfter is a channel's newest message id (0: none), and its
// messages after `after` whose text contains `contains` or that answer
// another (newest first, among the newest maxMessagesAfter), with their
// files: for unread marks, mention counts and Mumble's catch-up.
func (s *Store) MessagesAfter(channel, after int64, contains string) (int64, []Message, error) {
	var latest int64
	if err := s.db.QueryRow(`SELECT coalesce(max(id), 0) FROM messages WHERE channel_id = ?`, channel).Scan(&latest); err != nil {
		return 0, nil, err
	}
	// Only the newest maxMessagesAfter messages are looked at.
	rows, err := s.db.Query(messageSelect+`WHERE m.id IN (
			SELECT id FROM messages WHERE channel_id = ? AND id > ? ORDER BY id DESC LIMIT ?)
		AND (instr(m.text, ?) > 0 OR m.reply_to IS NOT NULL) ORDER BY m.id DESC`, channel, after, maxMessagesAfter, contains)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return 0, nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	rows.Close()
	return latest, out, withUploads(s.db, out) // Mumble's catch-up shows them
}

// DeleteUnusedUploads removes uploads created before `before` that were
// never attached, and returns their keys (the caller deletes the files).
func (s *Store) DeleteUnusedUploads(before time.Time) ([]string, error) {
	rows, err := s.db.Query(`DELETE FROM uploads WHERE used = 0 AND created_at < ? RETURNING key`, before.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// UploadedBytesSince sums the sizes of an author's uploads since a time
// (for the daily quota). Deleted unused uploads do not count.
func (s *Store) UploadedBytesSince(author string, since time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT coalesce(sum(size), 0) FROM uploads WHERE author = ? AND created_at >= ?`,
		author, since.UnixMilli()).Scan(&n)
	return n, err
}

var (
	ErrNotFound = errors.New("not found")
	// An edit would leave a message without text and files.
	ErrEmpty = errors.New("a message needs text or a file")
	// A message has reactions with maxReactionKinds different emoji.
	ErrTooManyReactions = errors.New("too many different reactions")
	ErrNameTaken        = errors.New("name taken")
	// Text channel names cannot have ":" (voice channel chats use it).
	ErrInvalidName = errors.New("invalid channel name")
)

// Account is a web user. Names compare by names.Key (case, NFC, spaces).
type Account struct {
	ID           int64
	Name         string
	PasswordHash string // empty for temporary accounts and fediverse-only ones
	CertPEM      []byte
	KeyPEM       []byte
	CreatedAt    time.Time
	// Temporary: a guest's account without a password, deleted after a
	// day without a visit (see IdleTemporaryAccounts).
	Temporary bool
}

// createAccount adds an account in tx. Ids are never given twice (other
// servers know accounts by id, see auth.Provider), so the highest id ever
// given is kept in store_meta.
func createAccount(tx *sql.Tx, a Account, at time.Time) (Account, error) {
	err := tx.QueryRow(`INSERT INTO store_meta (key, value) VALUES ('last_account_id', (SELECT coalesce(max(id), 0) FROM accounts) + 1)
		ON CONFLICT (key) DO UPDATE SET value = max(value, (SELECT coalesce(max(id), 0) FROM accounts)) + 1
		RETURNING value`).Scan(&a.ID)
	if err != nil {
		return Account{}, err
	}
	_, err = tx.Exec(`INSERT INTO accounts (id, name, name_key, password_hash, cert_pem, key_pem, created_at, temporary) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, names.Key(a.Name), a.PasswordHash, a.CertPEM, a.KeyPEM, at.UnixMilli(), a.Temporary)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Account{}, ErrNameTaken
		}
		return Account{}, err
	}
	a.CreatedAt = at
	return a, nil
}

const accountColumns = `a.id, a.name, a.password_hash, a.cert_pem, a.key_pem, a.created_at, a.temporary`

func scanAccount(row interface{ Scan(...any) error }) (Account, error) {
	return scanAccountWith(row)
}

// scanAccountWith reads an account and more columns after its own.
func scanAccountWith(row interface{ Scan(...any) error }, more ...any) (Account, error) {
	var a Account
	var at int64
	err := row.Scan(append([]any{&a.ID, &a.Name, &a.PasswordHash, &a.CertPEM, &a.KeyPEM, &at, &a.Temporary}, more...)...)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	a.CreatedAt = time.UnixMilli(at).UTC()
	return a, err
}

func (s *Store) AccountByName(name string) (Account, error) {
	return scanAccount(s.db.QueryRow(`SELECT `+accountColumns+` FROM accounts a WHERE a.name_key = ?`, names.Key(name)))
}

// Sessions are stored by the hash of their token: a leaked database does
// not give working session cookies.
func (s *Store) CreateSession(tokenHash string, accountID int64, expires time.Time) error {
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, account_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash, accountID, expires.UnixMilli())
	return err
}

// CreateSessionVouched creates a session only while the voucher (another
// session of the account, which approved it: a QR login) is valid, in one
// statement (a logout everywhere meanwhile ends it); else ErrNotFound.
func (s *Store) CreateSessionVouched(tokenHash string, accountID int64, expires time.Time, voucher string, now time.Time) error {
	return rowChanged(s.db.Exec(`INSERT INTO sessions (token_hash, account_id, expires_at)
		SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM sessions WHERE token_hash = ? AND account_id = ? AND expires_at > ?)`,
		tokenHash, accountID, expires.UnixMilli(), voucher, accountID, now.UnixMilli()))
}

func (s *Store) AccountBySession(tokenHash string, now time.Time) (Account, error) {
	return scanAccount(s.db.QueryRow(`SELECT `+accountColumns+` FROM sessions x JOIN accounts a ON a.id = x.account_id
		WHERE x.token_hash = ? AND x.expires_at > ?`, tokenHash, now.UnixMilli()))
}

// SessionExpires is when a session ends (ErrNotFound: none).
func (s *Store) SessionExpires(tokenHash string) (time.Time, error) {
	var at int64
	err := s.db.QueryRow(`SELECT expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	return time.UnixMilli(at), err
}

func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions(now time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// TouchAccount notes a visit (temporary accounts live a day from it).
func (s *Store) TouchAccount(id int64, at time.Time) error {
	_, err := s.db.Exec(`UPDATE accounts SET last_seen = ? WHERE id = ?`, at.UnixMilli(), id)
	return err
}

// ExpireAccount makes a temporary account idle at once (its owner logged
// out): the next cleanup removes it.
func (s *Store) ExpireAccount(id int64) error {
	_, err := s.db.Exec(`UPDATE accounts SET last_seen = 0 WHERE id = ? AND temporary = 1`, id)
	return err
}

// IdleTemporaryAccounts are temporary accounts not seen since before.
func (s *Store) IdleTemporaryAccounts(before time.Time) ([]Account, error) {
	rows, err := s.db.Query(`SELECT `+accountColumns+` FROM accounts a
		WHERE a.temporary = 1 AND coalesce(a.last_seen, a.created_at) < ?`, before.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// TemporaryAccounts counts them (there is a limit).
func (s *Store) TemporaryAccounts() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM accounts WHERE temporary = 1`).Scan(&n)
	return n, err
}

// TemporaryNames are the temporary accounts' names, as names.Key.
func (s *Store) TemporaryNames() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT name_key FROM accounts WHERE temporary = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = true
	}
	return out, rows.Err()
}

// MakePermanent gives a temporary account its password.
func (s *Store) MakePermanent(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE accounts SET password_hash = ?, temporary = 0 WHERE id = ?`, hash, id)
	return err
}

// DeleteIdleTemporaryAccount removes a temporary account not seen since
// before, with its sessions; deleted is false if it was seen or got a
// password meanwhile.
func (s *Store) DeleteIdleTemporaryAccount(id int64, before time.Time) (deleted bool, err error) {
	res, err := s.db.Exec(`DELETE FROM accounts WHERE id = ? AND temporary = 1 AND coalesce(last_seen, created_at) < ?`, id, before.UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteAccount removes an account and its sessions.
func (s *Store) DeleteAccount(id int64) error {
	_, err := s.db.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	return err
}

// ChangePassword gives the account a new password hash (a temporary account
// becomes permanent) and ends all its sessions but keep, in one transaction.
// ErrNotFound if keep is no longer a live session of the account or the hash
// is no longer oldHash: a change checked against an old snapshot.
func (s *Store) ChangePassword(accountID int64, keep, oldHash, newHash string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE accounts SET password_hash = ?, temporary = 0 WHERE id = ? AND password_hash = ?
		AND EXISTS (SELECT 1 FROM sessions WHERE token_hash = ? AND account_id = accounts.id AND expires_at > ?)`,
		newHash, accountID, oldHash, keep, now.UnixMilli())
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE account_id = ? AND token_hash != ?`, accountID, keep); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSessionsExcept ends all sessions of an account but one ("" ends all).
func (s *Store) DeleteSessionsExcept(accountID int64, keepTokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE account_id = ? AND token_hash != ?`, accountID, keepTokenHash)
	return err
}
