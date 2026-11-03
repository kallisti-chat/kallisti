package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// Registration is open unless an admin explicitly closes it. These tables
// are separate from channel invites: a channel link never admits a new account.
const registrationTables = `CREATE TABLE IF NOT EXISTS registration_policy (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	invite_only INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO registration_policy (id, invite_only) VALUES (1, 0);
CREATE TABLE IF NOT EXISTS registration_invites (
	id INTEGER PRIMARY KEY AUTOINCREMENT, -- not used again (a stale revoke)
	token_hash TEXT NOT NULL UNIQUE,
	creator TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	max_uses INTEGER NOT NULL CHECK (max_uses >= 0),
	uses INTEGER NOT NULL DEFAULT 0
)`

var ErrRegistrationInvite = errors.New("a valid account invitation is required")

type RegistrationInvite struct {
	ID      int64  `json:"id"`
	Creator string `json:"creator"`
	Created int64  `json:"created"`
	Expires int64  `json:"expires,omitempty"`
	MaxUses int    `json:"maxUses"`
	Uses    int    `json:"uses"`
}

func (s *Store) InviteOnly() (bool, error) {
	var on bool
	err := s.db.QueryRow(`SELECT invite_only FROM registration_policy WHERE id = 1`).Scan(&on)
	return on, err
}

func (s *Store) SetInviteOnly(on bool) error {
	_, err := s.db.Exec(`UPDATE registration_policy SET invite_only = ? WHERE id = 1`, on)
	return err
}

func registrationHash(token string) string {
	h := sha256.Sum256([]byte("registration:" + token))
	return hex.EncodeToString(h[:])
}

func (s *Store) AddRegistrationInvite(token string, i RegistrationInvite) error {
	_, err := s.db.Exec(`INSERT INTO registration_invites (token_hash, creator, created_at, expires_at, max_uses)
		VALUES (?, ?, ?, ?, ?)`, registrationHash(token), i.Creator, i.Created, i.Expires, i.MaxUses)
	return err
}

func (s *Store) RegistrationInvites() ([]RegistrationInvite, error) {
	rows, err := s.db.Query(`SELECT id, creator, created_at, expires_at, max_uses, uses FROM registration_invites ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RegistrationInvite{}
	for rows.Next() {
		var i RegistrationInvite
		if err := rows.Scan(&i.ID, &i.Creator, &i.Created, &i.Expires, &i.MaxUses, &i.Uses); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRegistrationInvite(id int64) error {
	_, err := s.db.Exec(`DELETE FROM registration_invites WHERE id = ?`, id)
	return err
}

// admit applies the policy: open admits everyone, an invitation is neither
// checked nor used then; invite-only needs a valid invitation for a
// permanent account. use: count one use (in the account's transaction, so a
// failed creation gives it back).
func admit(q interface {
	QueryRow(string, ...any) *sql.Row
	Exec(string, ...any) (sql.Result, error)
}, token string, temporary bool, at time.Time, use bool) error {
	var on bool
	if err := q.QueryRow(`SELECT invite_only FROM registration_policy WHERE id = 1`).Scan(&on); err != nil {
		return err
	}
	if !on {
		return nil
	}
	if token == "" || temporary {
		return ErrRegistrationInvite
	}
	var n int64
	var err error
	if use {
		var r sql.Result
		if r, err = q.Exec(`UPDATE registration_invites SET uses = uses + 1 WHERE token_hash = ? AND `+validInvite,
			registrationHash(token), at.UnixMilli()); err == nil {
			n, err = r.RowsAffected()
		}
	} else {
		err = q.QueryRow(`SELECT count(*) FROM registration_invites WHERE token_hash = ? AND `+validInvite,
			registrationHash(token), at.UnixMilli()).Scan(&n)
	}
	if err == nil && n != 1 {
		return ErrRegistrationInvite
	}
	return err
}

// CheckRegistration is the early check, before the expensive password or
// certificate work; the creation checks again.
func (s *Store) CheckRegistration(token string, temporary bool, at time.Time) error {
	return admit(s.db, token, temporary, at, false)
}

// CreateAccount stores a new account if the registration policy admits it
// (see admit).
func (s *Store) CreateAccount(a Account, at time.Time, invite string) (Account, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	if err := admit(tx, invite, a.Temporary, at, true); err != nil {
		return Account{}, err
	}
	if a, err = createAccount(tx, a, at); err != nil {
		return Account{}, err
	}
	return a, tx.Commit()
}
