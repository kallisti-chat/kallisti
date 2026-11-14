package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

const oidcTables = `CREATE TABLE IF NOT EXISTS oidc_identities (
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 account_id INTEGER NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
 created_at INTEGER NOT NULL,
 PRIMARY KEY (issuer, subject)
)`

var ErrOIDCTaken = errors.New("openid account already linked")

type OIDCIdentity struct{ Issuer, Subject string }

func linkOIDC(tx *sql.Tx, account int64, id OIDCIdentity, now time.Time) error {
	_, err := tx.Exec(`INSERT INTO oidc_identities (issuer, subject, account_id, created_at) VALUES (?, ?, ?, ?)`, id.Issuer, id.Subject, account, now.UnixMilli())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrOIDCTaken
	}
	return err
}

func (s *Store) CreateOIDCAccount(a Account, id OIDCIdentity, now time.Time, invite string) (Account, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Account{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = admit(tx, invite, false, now, true); err != nil {
		return Account{}, err
	}
	a.Temporary = false
	if a, err = createAccount(tx, a, now); err != nil {
		return Account{}, err
	}
	if err = linkOIDC(tx, a.ID, id, now); err != nil {
		return Account{}, err
	}
	return a, tx.Commit()
}

func (s *Store) OIDCIdentityOf(account int64) (OIDCIdentity, error) {
	var id OIDCIdentity
	err := s.db.QueryRow(`SELECT issuer, subject FROM oidc_identities WHERE account_id = ?`, account).Scan(&id.Issuer, &id.Subject)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return id, err
}

func (s *Store) AccountByOIDC(id OIDCIdentity) (Account, error) {
	return scanAccount(s.db.QueryRow(`SELECT `+accountColumns+` FROM accounts a JOIN oidc_identities i ON i.account_id = a.id WHERE i.issuer = ? AND i.subject = ?`, id.Issuer, id.Subject))
}

// linkoidc rechecks both the initiating session and the password snapshot.
func (s *Store) LinkOIDC(account int64, session, oldHash string, id OIDCIdentity, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM sessions x JOIN accounts a ON a.id = x.account_id WHERE x.token_hash = ? AND a.id = ? AND x.expires_at > ? AND a.password_hash = ?`, session, account, now.UnixMilli(), oldHash).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return ErrSessionGone
	}
	if err = linkOIDC(tx, account, id, now); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE accounts SET temporary = 0 WHERE id = ?`, account); err != nil {
		return err
	}
	return tx.Commit()
}

// unlinkoidc checks the password and session, removes the identity and ends
// other sessions atomically, retaining the password-confirmed current session.
func (s *Store) UnlinkOIDC(account int64, session, oldHash string, now time.Time) error {
	if oldHash == "" {
		return ErrLastLogin
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = rowChanged(tx.Exec(`DELETE FROM oidc_identities WHERE account_id = ? AND EXISTS
 (SELECT 1 FROM accounts a JOIN sessions x ON x.account_id = a.id WHERE a.id = ? AND a.password_hash = ? AND x.token_hash = ? AND x.expires_at > ?)`, account, account, oldHash, session, now.UnixMilli())); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE account_id = ? AND token_hash != ?`, account, session); err != nil {
		return err
	}
	return tx.Commit()
}

// createoidcsession cannot revive an identity removed after its lookup.
func (s *Store) CreateOIDCSession(hash string, account int64, id OIDCIdentity, expires time.Time) error {
	return rowChanged(s.db.Exec(`INSERT INTO sessions (token_hash, account_id, expires_at) SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM oidc_identities WHERE account_id = ? AND issuer = ? AND subject = ?)`, hash, account, expires.UnixMilli(), account, id.Issuer, id.Subject))
}
