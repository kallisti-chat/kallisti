// Package murmur reads a Murmur (mumble-server) database, schema version 9
// (Mumble 1.4 and 1.5), for an import into Kallisti (see mumbled.Import
// and the server's import-murmur command).
package murmur

import (
	"database/sql"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	"kallisti.chat/server/internal/mumbled"
)

// Murmur's ChannelInfo and UserInfo keys.
const (
	chanDescription = 0
	chanPosition    = 1
	chanMaxUsers    = 2
	userEmail       = 1
	userComment     = 2
	userHash        = 3
)

type ServerInfo struct {
	ID, Users, Channels int
	Name                string
}

// Servers lists the virtual servers (users and channels without
// SuperUser and root).
func Servers(db *sql.DB) ([]ServerInfo, error) {
	rows, err := db.Query(`SELECT server_id,
		coalesce((SELECT value FROM config c WHERE c.server_id = s.server_id AND key = 'registername'), ''),
		(SELECT count(*) FROM users u WHERE u.server_id = s.server_id AND user_id != 0),
		(SELECT count(*) FROM channels c WHERE c.server_id = s.server_id AND channel_id != 0)
		FROM servers s ORDER BY server_id`)
	if err != nil {
		return nil, fmt.Errorf("not a Murmur database? %w", err)
	}
	defer rows.Close()
	var out []ServerInfo
	for rows.Next() {
		var s ServerInfo
		if err := rows.Scan(&s.ID, &s.Name, &s.Users, &s.Channels); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Server is one virtual server, with Murmur's ids.
type Server struct {
	ID              int
	Name, Welcome   string
	CertPEM, KeyPEM string // its own (empty: the ini file's)
	DefaultChannel  bool   // set (Kallisti has none)
	Root            mumbled.Channel
	Channels        []mumbled.Channel
	Users           []mumbled.ImportUser
	NoCert          []string // users without a certificate (not imported)
	Bans            []mumbled.BanEntry
	Skipped         Skipped
}

// Skipped counts what Kallisti does not take.
type Skipped struct{ Passwords, Emails, UserLimits, Listeners int }

// Read reads one virtual server.
func Read(db *sql.DB, id int) (Server, error) {
	s := Server{ID: id}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM servers WHERE server_id = ?`, id).Scan(&n); err != nil {
		return s, fmt.Errorf("not a Murmur database? %w", err)
	}
	if n == 0 {
		return s, fmt.Errorf("no virtual server %d", id)
	}
	for _, step := range []func(*sql.DB, *Server) error{readConfig, readChannels, readACLs, readGroups, readUsers, readBans, readSkipped} {
		if err := step(db, &s); err != nil {
			return s, err
		}
	}
	return s, nil
}

// each runs fn for every row of a query.
func each(db *sql.DB, query string, args []any, fn func(*sql.Rows) error) error {
	rows, err := db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func readConfig(db *sql.DB, s *Server) error {
	return each(db, `SELECT key, coalesce(value, '') FROM config WHERE server_id = ?`, []any{s.ID}, func(r *sql.Rows) error {
		var k, v string
		if err := r.Scan(&k, &v); err != nil {
			return err
		}
		switch k {
		case "registername":
			s.Name = v
		case "welcometext":
			s.Welcome = v
		case "certificate":
			s.CertPEM = v
		case "key":
			s.KeyPEM = v
		case "defaultchannel":
			s.DefaultChannel = v != "" && v != "0"
		}
		return nil
	})
}

// channel: root (0) or one of s.Channels.
func (s *Server) channel(id uint32) *mumbled.Channel {
	if id == 0 {
		return &s.Root
	}
	for i := range s.Channels {
		if s.Channels[i].ID == id {
			return &s.Channels[i]
		}
	}
	return nil
}

func readChannels(db *sql.DB, s *Server) error {
	s.Root = mumbled.Channel{Name: "Root", Links: map[uint32]bool{}, Groups: map[string]*mumbled.Group{}}
	err := each(db, `SELECT channel_id, coalesce(parent_id, 0), coalesce(name, ''), coalesce(inheritacl, 1) FROM channels
		WHERE server_id = ? AND channel_id != 0 ORDER BY channel_id`, []any{s.ID}, func(r *sql.Rows) error {
		c := mumbled.Channel{Links: map[uint32]bool{}, Groups: map[string]*mumbled.Group{}}
		var inherit bool
		if err := r.Scan(&c.ID, &c.Parent, &c.Name, &inherit); err != nil {
			return err
		}
		c.NoInheritACL = !inherit
		s.Channels = append(s.Channels, c)
		return nil
	})
	if err != nil {
		return err
	}
	err = each(db, `SELECT channel_id, key, coalesce(value, '') FROM channel_info WHERE server_id = ?`, []any{s.ID}, func(r *sql.Rows) error {
		var id uint32
		var key int
		var v string
		if err := r.Scan(&id, &key, &v); err != nil {
			return err
		}
		c := s.channel(id)
		if c == nil {
			return nil
		}
		switch key {
		case chanDescription:
			c.Description = v
		case chanPosition:
			p, _ := strconv.Atoi(v)
			c.Position = int32(p)
		case chanMaxUsers:
			if n, _ := strconv.Atoi(v); n > 0 {
				s.Skipped.UserLimits++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return each(db, `SELECT channel_id, link_id FROM channel_links WHERE server_id = ?`, []any{s.ID}, func(r *sql.Rows) error {
		var a, b uint32
		if err := r.Scan(&a, &b); err != nil {
			return err
		}
		if ca, cb := s.channel(a), s.channel(b); ca != nil && cb != nil {
			ca.Links[b], cb.Links[a] = true, true
		}
		return nil
	})
}

func readACLs(db *sql.DB, s *Server) error {
	return each(db, `SELECT channel_id, user_id, coalesce(group_name, ''), coalesce(apply_here, 1), coalesce(apply_sub, 1),
		coalesce(grantpriv, 0), coalesce(revokepriv, 0) FROM acl WHERE server_id = ? ORDER BY channel_id, priority`, []any{s.ID}, func(r *sql.Rows) error {
		var ch uint32
		var user sql.NullInt64
		var a mumbled.ACL
		var allow, deny int64
		if err := r.Scan(&ch, &user, &a.Group, &a.ApplyHere, &a.ApplySubs, &allow, &deny); err != nil {
			return err
		}
		a.Allow, a.Deny = mumbled.Perm(allow)&mumbled.AllPerms, mumbled.Perm(deny)&mumbled.AllPerms
		if user.Valid && user.Int64 >= 0 {
			a.HasUser, a.UserID, a.Group = true, uint32(user.Int64), ""
		}
		if c := s.channel(ch); c != nil {
			c.ACLs = append(c.ACLs, a)
		}
		return nil
	})
}

func readGroups(db *sql.DB, s *Server) error {
	byID := map[int]*mumbled.Group{}
	err := each(db, `SELECT group_id, channel_id, coalesce(name, ''), coalesce(inherit, 1), coalesce(inheritable, 1) FROM groups WHERE server_id = ?`, []any{s.ID}, func(r *sql.Rows) error {
		var id int
		var ch uint32
		g := &mumbled.Group{Add: map[uint32]bool{}, Remove: map[uint32]bool{}}
		if err := r.Scan(&id, &ch, &g.Name, &g.Inherit, &g.Inheritable); err != nil {
			return err
		}
		if c := s.channel(ch); c != nil && g.Name != "" {
			c.Groups[g.Name] = g
			byID[id] = g
		}
		return nil
	})
	if err != nil {
		return err
	}
	return each(db, `SELECT group_id, user_id, coalesce(addit, 1) FROM group_members WHERE server_id = ?`, []any{s.ID}, func(r *sql.Rows) error {
		var id int
		var user uint32
		var add bool
		if err := r.Scan(&id, &user, &add); err != nil {
			return err
		}
		if g := byID[id]; g != nil {
			if add {
				g.Add[user] = true
			} else {
				g.Remove[user] = true
			}
		}
		return nil
	})
}

func readUsers(db *sql.DB, s *Server) error {
	info := map[uint32]map[int]string{}
	err := each(db, `SELECT user_id, key, coalesce(value, '') FROM user_info WHERE server_id = ?`, []any{s.ID}, func(r *sql.Rows) error {
		var id uint32
		var key int
		var v string
		if err := r.Scan(&id, &key, &v); err != nil {
			return err
		}
		if info[id] == nil {
			info[id] = map[int]string{}
		}
		info[id][key] = v
		return nil
	})
	if err != nil {
		return err
	}
	return each(db, `SELECT user_id, name, coalesce(pw, '') != '', coalesce(lastchannel, 0), texture FROM users
		WHERE server_id = ? AND user_id != 0 ORDER BY user_id`, []any{s.ID}, func(r *sql.Rows) error {
		var u mumbled.ImportUser
		var password bool
		if err := r.Scan(&u.ID, &u.Name, &password, &u.LastChannel, &u.Texture); err != nil {
			return err
		}
		u.CertHash, u.Comment = info[u.ID][userHash], info[u.ID][userComment]
		if u.CertHash == "" {
			s.NoCert = append(s.NoCert, u.Name)
			return nil
		}
		if password {
			s.Skipped.Passwords++
		}
		if info[u.ID][userEmail] != "" {
			s.Skipped.Emails++
		}
		if s.channel(u.LastChannel) == nil {
			u.LastChannel = 0
		}
		s.Users = append(s.Users, u)
		return nil
	})
}

func readBans(db *sql.DB, s *Server) error {
	return each(db, `SELECT base, coalesce(mask, 0), coalesce(name, ''), coalesce(hash, ''), coalesce(reason, ''), coalesce(start, ''), coalesce(duration, 0)
		FROM bans WHERE server_id = ?`, []any{s.ID}, func(r *sql.Rows) error {
		var base []byte
		var mask int
		var start string
		var seconds int64
		var b mumbled.BanEntry
		if err := r.Scan(&base, &mask, &b.Name, &b.Hash, &b.Reason, &start, &seconds); err != nil {
			return err
		}
		b.Prefix = banPrefix(base, mask)
		b.Duration = time.Duration(seconds) * time.Second
		b.Start = banStart(start)
		if b.Prefix.IsValid() || b.Hash != "" {
			s.Bans = append(s.Bans, b)
		}
		return nil
	})
}

// banStart: Murmur's start (UTC, ISO 8601, with or without a Z); none:
// now.
func banStart(v string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Now()
}

// banPrefix: Murmur keeps an IPv6 address (IPv4 mapped) and a mask over
// its 128 bits.
func banPrefix(base []byte, mask int) netip.Prefix {
	if len(base) != 16 || mask <= 0 || mask > 128 {
		return netip.Prefix{}
	}
	addr := netip.AddrFrom16([16]byte(base))
	if addr.Is4In6() && mask >= 96 {
		return netip.PrefixFrom(addr.Unmap(), mask-96).Masked()
	}
	return netip.PrefixFrom(addr, mask).Masked()
}

func readSkipped(db *sql.DB, s *Server) error {
	// Older schemas have no listeners table: then none.
	_ = db.QueryRow(`SELECT count(*) FROM channel_listeners WHERE server_id = ?`, s.ID).Scan(&s.Skipped.Listeners)
	return nil
}
