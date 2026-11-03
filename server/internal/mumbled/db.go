package mumbled

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/netip"
	"time"

	"kallisti.chat/server/internal/msgrules"
	"kallisti.chat/server/internal/names"
)

// DB stores the Mumble server's data in the Kallisti server's SQLite file.
type DB struct{ db *sql.DB }

const dbSchema = `
CREATE TABLE IF NOT EXISTS mumble_channels (
	id          INTEGER PRIMARY KEY,
	parent      INTEGER NOT NULL,
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	position    INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS mumble_acl (
	channel_id INTEGER NOT NULL,
	priority   INTEGER NOT NULL,
	apply_here INTEGER NOT NULL,
	apply_subs INTEGER NOT NULL,
	user_id    INTEGER,
	group_name TEXT NOT NULL DEFAULT '',
	allow      INTEGER NOT NULL,
	deny       INTEGER NOT NULL,
	PRIMARY KEY (channel_id, priority)
);
CREATE TABLE IF NOT EXISTS mumble_groups (
	channel_id  INTEGER NOT NULL,
	name        TEXT NOT NULL,
	inherit     INTEGER NOT NULL,
	inheritable INTEGER NOT NULL,
	PRIMARY KEY (channel_id, name)
);
CREATE TABLE IF NOT EXISTS mumble_group_members (
	channel_id INTEGER NOT NULL,
	group_name TEXT NOT NULL,
	user_id    INTEGER NOT NULL,
	removed    INTEGER NOT NULL, -- 0: added, 1: removed from inherited members
	PRIMARY KEY (channel_id, group_name, user_id)
);
CREATE TABLE IF NOT EXISTS mumble_bans (
	id       INTEGER PRIMARY KEY,
	prefix   TEXT NOT NULL DEFAULT '', -- "10.0.0.0/8", or '' for none
	hash     TEXT NOT NULL DEFAULT '',
	name     TEXT NOT NULL DEFAULT '',
	reason   TEXT NOT NULL DEFAULT '',
	start    INTEGER NOT NULL,
	duration INTEGER NOT NULL -- seconds, 0: forever
);
CREATE TABLE IF NOT EXISTS mumble_meta (
	key   TEXT PRIMARY KEY,
	value INTEGER NOT NULL
);
-- Root has a row too: its description, and links to it.
INSERT OR IGNORE INTO mumble_channels (id, parent, name) VALUES (0, 0, 'Root');
CREATE TABLE IF NOT EXISTS mumble_users (
	id            INTEGER PRIMARY KEY,
	name          TEXT NOT NULL,
	name_key      TEXT NOT NULL UNIQUE,
	cert_hash     TEXT UNIQUE,
	password_hash TEXT NOT NULL DEFAULT '',
	last_channel  INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO mumble_users (id, name, name_key) VALUES (0, 'SuperUser', 'superuser');
-- Kallisti time-outs of registered users.
CREATE TABLE IF NOT EXISTS mumble_timeouts (
	user_id INTEGER PRIMARY KEY,
	until   INTEGER NOT NULL -- Unix milliseconds
);
-- Kallisti channel time-outs of registered users (no foreign key:
-- temporary channels are not saved).
CREATE TABLE IF NOT EXISTS mumble_channel_timeouts (
	user_id    INTEGER NOT NULL,
	channel_id INTEGER NOT NULL,
	until      INTEGER NOT NULL, -- Unix milliseconds
	PRIMARY KEY (user_id, channel_id)
);
-- Settings that are no numbers (see mumble_meta), as JSON.
CREATE TABLE IF NOT EXISTS mumble_settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS mumble_channel_links (
	a INTEGER NOT NULL REFERENCES mumble_channels(id) ON DELETE CASCADE,
	b INTEGER NOT NULL REFERENCES mumble_channels(id) ON DELETE CASCADE,
	PRIMARY KEY (a, b)
);
`

// Columns added after the first version, for databases made before; fill
// sets a new column for the rows there are.
var migrations = []struct {
	table, column, def string
	fill               func(*sql.Tx) error
}{
	{"mumble_channels", "no_inherit_acl", "INTEGER NOT NULL DEFAULT 0", nil},
	{"mumble_users", "comment", "TEXT NOT NULL DEFAULT ''", nil},
	{"mumble_users", "texture", "BLOB", nil},
	// Unix milliseconds; 0: registered before it was kept (an old account).
	{"mumble_users", "registered_at", "INTEGER NOT NULL DEFAULT 0", nil},
	// 1: the texture is an image the server serves (see Pictures).
	{"mumble_users", "picture", "INTEGER NOT NULL DEFAULT 0", flagPictures},
}

func OpenDB(db *sql.DB) (*DB, error) {
	if _, err := db.Exec(dbSchema); err != nil {
		return nil, err
	}
	for _, m := range migrations {
		if err := addColumn(db, m.table, m.column, m.def, m.fill); err != nil {
			return nil, err
		}
	}
	if err := recheckPictures(db); err != nil {
		return nil, err
	}
	d := &DB{db}
	return d, d.initACL()
}

// recheckPictures sets the picture flags again, once: flags set before
// the size check (see avatarType) can mark avatars Avatars does not serve.
func recheckPictures(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM mumble_meta WHERE key = 'pictures_checked'`).Scan(&done); err != nil || done > 0 {
		return err
	}
	if err := flagPictures(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO mumble_meta (key, value) VALUES ('pictures_checked', 1)`); err != nil {
		return err
	}
	return tx.Commit()
}

// addColumn adds a column if it is missing and fills it, in one
// transaction (a failed fill runs again at the next start).
func addColumn(db *sql.DB, table, column, def string, fill func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n); err != nil || n > 0 {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + def); err != nil {
		return err
	}
	if fill != nil {
		if err := fill(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// initACL gives a new database Murmur's default root ACL, once.
func (d *DB) initACL() error {
	var done int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM mumble_meta WHERE key = 'acl_initialized'`).Scan(&done)
	if err != nil || done > 0 {
		return err
	}
	root := &Channel{ID: 0, Name: "Root", Links: map[uint32]bool{}}
	root.ACLs, root.Groups = DefaultRootACL()
	if err := d.SaveChannel(root); err != nil {
		return err
	}
	_, err = d.db.Exec(`INSERT INTO mumble_meta (key, value) VALUES ('acl_initialized', 1)`)
	return err
}

// Channels returns the saved channels with their links.
func (d *DB) Channels() ([]Channel, error) {
	rows, err := d.db.Query(`SELECT id, parent, name, description, position, no_inherit_acl FROM mumble_channels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[uint32]*Channel{}
	var order []uint32
	for rows.Next() {
		c := &Channel{Links: map[uint32]bool{}, Groups: map[string]*Group{}}
		if err := rows.Scan(&c.ID, &c.Parent, &c.Name, &c.Description, &c.Position, &c.NoInheritACL); err != nil {
			return nil, err
		}
		byID[c.ID] = c
		order = append(order, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	links, err := d.db.Query(`SELECT a, b FROM mumble_channel_links`)
	if err != nil {
		return nil, err
	}
	defer links.Close()
	for links.Next() {
		var a, b uint32
		if err := links.Scan(&a, &b); err != nil {
			return nil, err
		}
		if byID[a] != nil {
			byID[a].Links[b] = true
		}
	}
	if err := links.Err(); err != nil {
		return nil, err
	}
	if err := d.loadACLs(byID); err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func (d *DB) loadACLs(byID map[uint32]*Channel) error {
	rows, err := d.db.Query(`SELECT channel_id, apply_here, apply_subs, user_id, group_name, allow, deny
		FROM mumble_acl ORDER BY channel_id, priority`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ch uint32
		var a ACL
		var user sql.NullInt64
		if err := rows.Scan(&ch, &a.ApplyHere, &a.ApplySubs, &user, &a.Group, &a.Allow, &a.Deny); err != nil {
			return err
		}
		a.HasUser, a.UserID = user.Valid, uint32(user.Int64)
		if c := byID[ch]; c != nil {
			c.ACLs = append(c.ACLs, a)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	groups, err := d.db.Query(`SELECT channel_id, name, inherit, inheritable FROM mumble_groups`)
	if err != nil {
		return err
	}
	defer groups.Close()
	for groups.Next() {
		var ch uint32
		g := newGroup("")
		if err := groups.Scan(&ch, &g.Name, &g.Inherit, &g.Inheritable); err != nil {
			return err
		}
		if c := byID[ch]; c != nil {
			c.Groups[g.Name] = g
		}
	}
	if err := groups.Err(); err != nil {
		return err
	}
	members, err := d.db.Query(`SELECT channel_id, group_name, user_id, removed FROM mumble_group_members`)
	if err != nil {
		return err
	}
	defer members.Close()
	for members.Next() {
		var ch, user uint32
		var name string
		var removed bool
		if err := members.Scan(&ch, &name, &user, &removed); err != nil {
			return err
		}
		if c := byID[ch]; c != nil && c.Groups[name] != nil {
			if removed {
				c.Groups[name].Remove[user] = true
			} else {
				c.Groups[name].Add[user] = true
			}
		}
	}
	return members.Err()
}

// SaveChannel writes a channel and its links (each link once, a < b).
func (d *DB) SaveChannel(c *Channel) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveChannel(tx, c); err != nil {
		return err
	}
	return tx.Commit()
}

func saveChannel(tx *sql.Tx, c *Channel) error {
	if _, err := tx.Exec(`INSERT INTO mumble_channels (id, parent, name, description, position, no_inherit_acl) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET parent = excluded.parent, name = excluded.name,
		description = excluded.description, position = excluded.position, no_inherit_acl = excluded.no_inherit_acl`,
		c.ID, c.Parent, c.Name, c.Description, c.Position, c.NoInheritACL); err != nil {
		return err
	}
	if err := saveACLs(tx, c); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM mumble_channel_links WHERE a = ? OR b = ?`, c.ID, c.ID); err != nil {
		return err
	}
	for l := range c.Links {
		a, b := min(c.ID, l), max(c.ID, l)
		// The other channel may not be saved yet (it is saved next).
		if _, err := tx.Exec(`INSERT OR IGNORE INTO mumble_channel_links (a, b)
			SELECT ?, ? WHERE EXISTS (SELECT 1 FROM mumble_channels WHERE id = ?)`, a, b, l); err != nil {
			return err
		}
	}
	return nil
}

// DeleteChannels removes channels (a subtree) at once.
func (d *DB) DeleteChannels(ids []uint32) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(`DELETE FROM mumble_channel_links WHERE a = ? OR b = ?`, id, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM mumble_channels WHERE id = ? AND id != 0`, id); err != nil {
			return err
		}
		if id != 0 {
			if err := deleteACLs(tx, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// NextChannel is the highest channel id given out, so ids of removed and
// temporary channels are not reused.
func (d *DB) NextChannel() (uint32, error) { return d.meta("last_channel_id") }

// meta is a number in mumble_meta (0: none yet).
func (d *DB) meta(key string) (uint32, error) {
	var n uint32
	err := d.db.QueryRow(`SELECT value FROM mumble_meta WHERE key = ?`, key).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

func (d *DB) setMeta(key string, n uint32) error {
	_, err := d.db.Exec(`INSERT INTO mumble_meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, n)
	return err
}

// TextParent is the parent of the text channel mirrors (0: none yet).
func (d *DB) TextParent() (uint32, error) { return d.meta("text_parent") }

func (d *DB) SetTextParent(id uint32) error { return d.setMeta("text_parent", id) }

// GuestsReadOnly is the setting (see Server.SetGuestsReadOnly).
func (d *DB) GuestsReadOnly() (bool, error) {
	n, err := d.meta("guests_read_only")
	return n != 0, err
}

func (d *DB) SetGuestsReadOnly(on bool) error {
	var n uint32
	if on {
		n = 1
	}
	return d.setMeta("guests_read_only", n)
}

// setting reads a JSON value of mumble_settings into v (false: none).
func (d *DB) setting(key string, v any) (bool, error) {
	var raw string
	err := d.db.QueryRow(`SELECT value FROM mumble_settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}

func (d *DB) setSetting(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`INSERT INTO mumble_settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, string(raw))
	return err
}

// MessageRules are the saved rules (see Server.SetMessageRules).
func (d *DB) MessageRules() (msgrules.Rules, error) {
	var r msgrules.Rules
	_, err := d.setting("message_rules", &r)
	return r, err
}

func (d *DB) SetMessageRules(r msgrules.Rules) error { return d.setSetting("message_rules", r) }

// ServerName is the name an admin set ("": none, see Server.SetServerName).
func (d *DB) ServerName() (string, error) {
	var name string
	_, err := d.setting("server_name", &name)
	return name, err
}

func (d *DB) SetServerName(name string) error { return d.setSetting("server_name", name) }

func (d *DB) SaveNextChannel(n uint32) error {
	_, err := d.db.Exec(`INSERT INTO mumble_meta (key, value) VALUES ('last_channel_id', ?)
		ON CONFLICT (key) DO UPDATE SET value = max(value, excluded.value)`, n)
	return err
}

const userColumns = `id, name, COALESCE(cert_hash, ''), password_hash, last_channel, registered_at`

func scanUser(row interface{ Scan(...any) error }) (Registered, error) {
	var r Registered
	var since int64
	err := row.Scan(&r.ID, &r.Name, &r.CertHash, &r.PasswordHash, &r.LastChannel, &since)
	if since > 0 {
		r.Since = time.UnixMilli(since)
	}
	return r, err
}

func (d *DB) userWhere(where string, arg any) (*Registered, error) {
	r, err := scanUser(d.db.QueryRow(`SELECT `+userColumns+` FROM mumble_users WHERE `+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UserByName finds a registered user by name (compared as names.Key).
func (d *DB) UserByName(name string) (*Registered, error) {
	return d.userWhere(`name_key = ?`, names.Key(name))
}

// CheckPassword logs in a registration that has a password (SuperUser,
// for example) and returns its name.
func (d *DB) CheckPassword(name, pw string) (string, bool) {
	r, err := d.UserByName(name)
	if err != nil || r == nil || r.PasswordHash == "" {
		return "", false
	}
	return r.Name, checkPassword(r.PasswordHash, pw)
}

func (d *DB) UserByID(id uint32) (*Registered, error) {
	return d.userWhere(`id = ?`, id)
}

func (d *DB) UserByHash(hash string) (*Registered, error) {
	if hash == "" {
		return nil, nil
	}
	return d.userWhere(`cert_hash = ?`, hash)
}

// RegisterUser adds a registration. Ids are never used twice (removed
// guest accounts' ids stay theirs: reactions and messages refer to them),
// so the highest id ever given is kept in mumble_meta.
func (d *DB) RegisterUser(name, certHash string) (uint32, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id uint32
	err = tx.QueryRow(`SELECT max(coalesce((SELECT value FROM mumble_meta WHERE key = 'last_user_id'), 0),
		(SELECT coalesce(max(id), 0) FROM mumble_users)) + 1`).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO mumble_users (id, name, name_key, cert_hash, registered_at) VALUES (?, ?, ?, NULLIF(?, ''), ?)`,
		id, name, names.Key(name), certHash, time.Now().UnixMilli()); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO mumble_meta (key, value) VALUES ('last_user_id', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, id); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (d *DB) SetPassword(id uint32, passwordHash string) error {
	_, err := d.db.Exec(`UPDATE mumble_users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

func (d *DB) SetLastChannel(id, channel uint32) error {
	_, err := d.db.Exec(`UPDATE mumble_users SET last_channel = ? WHERE id = ?`, channel, id)
	return err
}

// UserNames returns the names of the registered users among ids.
func (d *DB) UserNames(ids []uint32) (map[uint32]string, error) {
	out := map[uint32]string{}
	for _, id := range ids {
		var name string
		err := d.db.QueryRow(`SELECT name FROM mumble_users WHERE id = ?`, id).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, nil
}

func deleteACLs(tx *sql.Tx, channel uint32) error {
	for _, table := range []string{"mumble_acl", "mumble_groups", "mumble_group_members"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE channel_id = ?`, channel); err != nil {
			return err
		}
	}
	return nil
}

// saveACLs replaces a channel's ACL entries and groups.
func saveACLs(tx *sql.Tx, c *Channel) error {
	if err := deleteACLs(tx, c.ID); err != nil {
		return err
	}
	for i, a := range c.ACLs {
		var user any
		if a.HasUser {
			user = a.UserID
		}
		if _, err := tx.Exec(`INSERT INTO mumble_acl (channel_id, priority, apply_here, apply_subs, user_id, group_name, allow, deny)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, i, a.ApplyHere, a.ApplySubs, user, a.Group, a.Allow, a.Deny); err != nil {
			return err
		}
	}
	for _, g := range c.Groups {
		if _, err := tx.Exec(`INSERT INTO mumble_groups (channel_id, name, inherit, inheritable) VALUES (?, ?, ?, ?)`,
			c.ID, g.Name, g.Inherit, g.Inheritable); err != nil {
			return err
		}
		for removed, ids := range []map[uint32]bool{g.Add, g.Remove} {
			for id := range ids {
				if _, err := tx.Exec(`INSERT OR REPLACE INTO mumble_group_members (channel_id, group_name, user_id, removed)
					VALUES (?, ?, ?, ?)`, c.ID, g.Name, id, removed); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Users lists the registered users without SuperUser.
func (d *DB) Users() ([]Registered, error) {
	rows, err := d.db.Query(`SELECT ` + userColumns + ` FROM mumble_users WHERE id != 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Registered
	for rows.Next() {
		r, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) RenameUser(id uint32, name string) error {
	if why := names.Check(name); why != "" || id == 0 {
		return errors.New("invalid name")
	}
	_, err := d.db.Exec(`UPDATE mumble_users SET name = ?, name_key = ? WHERE id = ?`, name, names.Key(name), id)
	return err
}

// UnregisterUser removes a user and its ACL entries and group memberships.
func (d *DB) UnregisterUser(id uint32) error {
	if id == 0 {
		return errors.New("SuperUser cannot be removed")
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM mumble_users WHERE id = ?`,
		`DELETE FROM mumble_acl WHERE user_id = ?`,
		`DELETE FROM mumble_group_members WHERE user_id = ?`,
		`DELETE FROM mumble_timeouts WHERE user_id = ?`,
		`DELETE FROM mumble_channel_timeouts WHERE user_id = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) Bans() ([]BanEntry, error) {
	rows, err := d.db.Query(`SELECT prefix, hash, name, reason, start, duration FROM mumble_bans ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BanEntry
	for rows.Next() {
		var b BanEntry
		var prefix string
		var start, duration int64
		if err := rows.Scan(&prefix, &b.Hash, &b.Name, &b.Reason, &start, &duration); err != nil {
			return nil, err
		}
		if prefix != "" {
			if b.Prefix, err = netip.ParsePrefix(prefix); err != nil {
				return nil, err
			}
		}
		b.Start, b.Duration = time.Unix(start, 0).UTC(), time.Duration(duration)*time.Second
		out = append(out, b)
	}
	return out, rows.Err()
}

// SaveBans replaces the ban list.
func (d *DB) SaveBans(bans []BanEntry) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveBans(tx, bans); err != nil {
		return err
	}
	return tx.Commit()
}

func saveBans(tx *sql.Tx, bans []BanEntry) error {
	if _, err := tx.Exec(`DELETE FROM mumble_bans`); err != nil {
		return err
	}
	for _, b := range bans {
		prefix := ""
		if b.Prefix.IsValid() {
			prefix = b.Prefix.String()
		}
		if _, err := tx.Exec(`INSERT INTO mumble_bans (prefix, hash, name, reason, start, duration) VALUES (?, ?, ?, ?, ?, ?)`,
			prefix, b.Hash, b.Name, b.Reason, b.Start.Unix(), int64(b.Duration/time.Second)); err != nil {
			return err
		}
	}
	return nil
}

// Timeouts: the saved time-outs, by user id.
func (d *DB) Timeouts() (map[uint32]time.Time, error) {
	rows, err := d.db.Query(`SELECT user_id, until FROM mumble_timeouts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint32]time.Time{}
	for rows.Next() {
		var id uint32
		var until int64
		if err := rows.Scan(&id, &until); err != nil {
			return nil, err
		}
		out[id] = time.UnixMilli(until)
	}
	return out, rows.Err()
}

// SaveTimeout keeps a user's time-out (zero: none).
func (d *DB) SaveTimeout(id uint32, until time.Time) error {
	if until.IsZero() {
		_, err := d.db.Exec(`DELETE FROM mumble_timeouts WHERE user_id = ?`, id)
		return err
	}
	_, err := d.db.Exec(`INSERT INTO mumble_timeouts (user_id, until) VALUES (?, ?)
		ON CONFLICT (user_id) DO UPDATE SET until = excluded.until`, id, until.UnixMilli())
	return err
}

// UserChannel names a registered user's channel time-out.
type UserChannel struct{ User, Channel uint32 }

// ChannelTimeouts: the saved channel time-outs.
func (d *DB) ChannelTimeouts() (map[UserChannel]time.Time, error) {
	rows, err := d.db.Query(`SELECT user_id, channel_id, until FROM mumble_channel_timeouts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[UserChannel]time.Time{}
	for rows.Next() {
		var uc UserChannel
		var until int64
		if err := rows.Scan(&uc.User, &uc.Channel, &until); err != nil {
			return nil, err
		}
		out[uc] = time.UnixMilli(until)
	}
	return out, rows.Err()
}

// SaveChannelTimeout keeps a channel time-out (zero: none).
func (d *DB) SaveChannelTimeout(uc UserChannel, until time.Time) error {
	if until.IsZero() {
		_, err := d.db.Exec(`DELETE FROM mumble_channel_timeouts WHERE user_id = ? AND channel_id = ?`, uc.User, uc.Channel)
		return err
	}
	_, err := d.db.Exec(`INSERT INTO mumble_channel_timeouts (user_id, channel_id, until) VALUES (?, ?, ?)
		ON CONFLICT (user_id, channel_id) DO UPDATE SET until = excluded.until`, uc.User, uc.Channel, until.UnixMilli())
	return err
}

// UserContent is a registered user's comment and avatar (read at login
// only: avatars are large).
func (d *DB) UserContent(id uint32) (comment string, texture []byte, err error) {
	err = d.db.QueryRow(`SELECT comment, texture FROM mumble_users WHERE id = ?`, id).Scan(&comment, &texture)
	return comment, texture, err
}

// comment is a registered user's comment ("": none, or no such user).
func (d *DB) comment(id uint32) (string, error) {
	var comment string
	err := d.db.QueryRow(`SELECT comment FROM mumble_users WHERE id = ?`, id).Scan(&comment)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return comment, err
}

// textureByName is a registered user's avatar (nil: none, or no such user).
func (d *DB) textureByName(name string) ([]byte, error) {
	var texture []byte
	err := d.db.QueryRow(`SELECT texture FROM mumble_users WHERE name_key = ?`, names.Key(name)).Scan(&texture)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return texture, err
}

// SetUserContent saves a registered user's comment and avatar, and
// whether the avatar is a picture (see Pictures).
func (d *DB) SetUserContent(id uint32, comment string, texture []byte) error {
	_, err := d.db.Exec(`UPDATE mumble_users SET comment = ?, texture = ?, picture = ? WHERE id = ?`,
		comment, texture, avatarType(texture) != "", id)
	return err
}

// Pictures: the registered users whose avatar Avatars serves, by id
// (without reading the avatars).
func (d *DB) Pictures() (map[uint32]bool, error) {
	rows, err := d.db.Query(`SELECT id FROM mumble_users WHERE picture != 0 AND id != 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint32]bool{}
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// flagPictures sets the picture column of saved avatars (see migrations).
func flagPictures(tx *sql.Tx) error {
	if _, err := tx.Exec(`UPDATE mumble_users SET picture = 0`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id, texture FROM mumble_users WHERE length(texture) > 0`)
	if err != nil {
		return err
	}
	var ids []uint32
	for rows.Next() {
		var id uint32
		var texture []byte
		if err := rows.Scan(&id, &texture); err != nil {
			rows.Close()
			return err
		}
		if avatarType(texture) != "" {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE mumble_users SET picture = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// InitLobby makes root a lobby without voice, as Kallisti servers have it:
// users arrive there and join voice channels on purpose. A new server gets
// a "General" voice channel. Runs once per database.
// InitRoles gives an older database the moderator group and its ACL in
// root, once.
func (d *DB) InitRoles() error {
	var done int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM mumble_meta WHERE key = 'roles'`).Scan(&done); err != nil || done > 0 {
		return err
	}
	saved, err := d.Channels()
	if err != nil {
		return err
	}
	root := NewState("Root", saved).Channels[0]
	if root.Groups["moderator"] == nil {
		if root.Groups == nil {
			root.Groups = map[string]*Group{}
		}
		root.Groups["moderator"] = newGroup("moderator")
		root.ACLs = append(root.ACLs, moderatorACL()...)
		if err := d.SaveChannel(root); err != nil {
			return err
		}
	}
	_, err = d.db.Exec(`INSERT INTO mumble_meta (key, value) VALUES ('roles', 1)`)
	return err
}

func (d *DB) InitLobby() error {
	var done int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM mumble_meta WHERE key = 'lobby'`).Scan(&done); err != nil || done > 0 {
		return err
	}
	saved, err := d.Channels()
	if err != nil {
		return err
	}
	s := NewState("Root", saved)
	root := s.Channels[0]
	root.ACLs = append(root.ACLs, ACL{ApplyHere: true, ApplySubs: false, Group: "all", Deny: Speak})
	if err := d.SaveChannel(root); err != nil {
		return err
	}
	if len(s.Channels) == 1 {
		last, err := d.NextChannel()
		if err != nil {
			return err
		}
		s.SetLastChannelID(last)
		general, err := s.AddChannel(0, "General", false)
		if err != nil {
			return err
		}
		if err := d.SaveChannel(general); err != nil {
			return err
		}
		if err := d.SaveNextChannel(general.ID); err != nil {
			return err
		}
	}
	_, err = d.db.Exec(`INSERT INTO mumble_meta (key, value) VALUES ('lobby', 1)`)
	return err
}
