package mumbled

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"kallisti.chat/server/internal/names"
)

// Import is another server's state for a fresh one (see internal/murmur).
// Channel ids count from 1 among themselves (parent 0: root); DB.Import
// gives them ids after the existing channels. User ids stay.
type Import struct {
	// Root: the server's root (ACLs, groups, description) as ours, made a
	// lobby; nil: ours stays and the channels go under it.
	Root       *Channel
	Channels   []Channel
	Users      []ImportUser
	Bans       []BanEntry
	ServerName string // "": ours stays
}

type ImportUser struct {
	ID                      uint32
	Name, CertHash, Comment string
	Texture                 []byte
	LastChannel             uint32 // among the imported channels (0: root)
}

// Import writes it, in one transaction, into a fresh server: no
// registered users but SuperUser, no channels but root, the text
// channels' mirrors (they stay) and the automatic General of the lobby
// (it goes).
func (d *DB) Import(imp Import) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	general, err := freshCheck(tx)
	if err != nil {
		return err
	}
	if general != 0 {
		if _, err := tx.Exec(`DELETE FROM mumble_channels WHERE id = ?`, general); err != nil {
			return err
		}
		if err := deleteACLs(tx, general); err != nil {
			return err
		}
	}
	var last uint32
	if err := tx.QueryRow(`SELECT max(coalesce((SELECT value FROM mumble_meta WHERE key = 'last_channel_id'), 0), (SELECT max(id) FROM mumble_channels))`).Scan(&last); err != nil {
		return err
	}
	id := func(c uint32) uint32 {
		if c == 0 {
			return 0
		}
		return c + last
	}
	next := last
	for _, c := range imp.Channels {
		c.ID, c.Parent = id(c.ID), id(c.Parent)
		links := map[uint32]bool{}
		for l := range c.Links {
			links[id(l)] = true
		}
		c.Links = links
		if err := saveChannel(tx, &c); err != nil {
			return fmt.Errorf("channel %q: %w", c.Name, err)
		}
		next = max(next, c.ID)
	}
	// Links to channels saved later: again, now that all exist.
	for _, c := range imp.Channels {
		for l := range c.Links {
			a, b := min(id(c.ID), id(l)), max(id(c.ID), id(l))
			if _, err := tx.Exec(`INSERT OR IGNORE INTO mumble_channel_links (a, b) VALUES (?, ?)`, a, b); err != nil {
				return err
			}
		}
	}
	if err := setMetaTx(tx, "last_channel_id", next); err != nil {
		return err
	}
	if imp.Root != nil {
		// The server's root, as a lobby (see InitLobby, which then adds no
		// General); the moderator role at the next start (InitRoles).
		root := *imp.Root
		root.ID, root.Parent, root.Name = 0, 0, "Root"
		root.ACLs = append(slices.Clone(root.ACLs), ACL{ApplyHere: true, Group: "all", Deny: Speak})
		if err := saveChannel(tx, &root); err != nil {
			return err
		}
		if err := setMetaTx(tx, "lobby", 1); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM mumble_meta WHERE key = 'roles'`); err != nil {
			return err
		}
	}
	now := time.Now().UnixMilli()
	for _, u := range imp.Users {
		var hash any
		if u.CertHash != "" {
			hash = u.CertHash
		}
		if _, err := tx.Exec(`INSERT INTO mumble_users (id, name, name_key, cert_hash, last_channel, comment, texture, picture, registered_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, u.Name, names.Key(u.Name), hash, id(u.LastChannel), u.Comment, u.Texture, avatarType(u.Texture) != "", now); err != nil {
			return fmt.Errorf("user %q: %w", u.Name, err)
		}
	}
	if err := saveBans(tx, imp.Bans); err != nil {
		return err
	}
	if imp.ServerName != "" {
		name, _ := json.Marshal(imp.ServerName)
		if _, err := tx.Exec(`INSERT INTO mumble_settings (key, value) VALUES ('server_name', ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, string(name)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// freshCheck: no registered users but SuperUser, no channels but root, the
// text mirrors and the lobby's automatic General (its id, 0: none).
func freshCheck(tx *sql.Tx) (general uint32, err error) {
	var users int
	if err := tx.QueryRow(`SELECT count(*) FROM mumble_users WHERE id != 0`).Scan(&users); err != nil {
		return 0, err
	}
	if users > 0 {
		return 0, fmt.Errorf("this server has %d registered users: import only into a new one", users)
	}
	rows, err := tx.Query(`SELECT id, parent, name FROM mumble_channels WHERE id != 0`)
	if err != nil {
		return 0, err
	}
	type ch struct {
		id, parent uint32
		name       string
	}
	var all []ch
	for rows.Next() {
		var c ch
		if err := rows.Scan(&c.id, &c.parent, &c.name); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var textParent, lobby uint32
	for key, v := range map[string]*uint32{"text_parent": &textParent, "lobby": &lobby} {
		if err := tx.QueryRow(`SELECT value FROM mumble_meta WHERE key = ?`, key).Scan(v); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	mirror := map[uint32]bool{}
	if textParent != 0 {
		mirror[textParent] = true
		for grown := true; grown; {
			grown = false
			for _, c := range all {
				if !mirror[c.id] && mirror[c.parent] {
					mirror[c.id], grown = true, true
				}
			}
		}
	}
	var own []string
	for _, c := range all {
		switch {
		case mirror[c.id]:
		case lobby != 0 && general == 0 && c.parent == 0 && c.name == "General" && !hasACLs(tx, c.id):
			general = c.id
		default:
			own = append(own, c.name)
		}
	}
	if len(own) > 0 {
		slices.Sort(own)
		return 0, fmt.Errorf("this server has its own channels (%s): import only into a new one", strings.Join(own, ", "))
	}
	return general, nil
}

func hasACLs(tx *sql.Tx, channel uint32) bool {
	var n int
	err := tx.QueryRow(`SELECT (SELECT count(*) FROM mumble_acl WHERE channel_id = ?) + (SELECT count(*) FROM mumble_groups WHERE channel_id = ?)`, channel, channel).Scan(&n)
	return err != nil || n > 0
}

func setMetaTx(tx *sql.Tx, key string, value uint32) error {
	_, err := tx.Exec(`INSERT INTO mumble_meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
