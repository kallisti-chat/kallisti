package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

// Recovery links: an admin's one-time link to set a new password (a
// forgotten one, or a login that went away). One per account, stored by
// the hash of the token, like sessions.
const recoveryTable = `CREATE TABLE IF NOT EXISTS recovery_links (
	account_id INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
	token_hash TEXT NOT NULL UNIQUE,
	expires_at INTEGER NOT NULL
);
-- A Mumble registration without an account (imported from Murmur, or made
-- in a Mumble app): its link makes the account when used (see Claim).
CREATE TABLE IF NOT EXISTS claim_links (
	mumble_user INTEGER PRIMARY KEY,
	name        TEXT NOT NULL,
	cert_hash   TEXT NOT NULL,
	token_hash  TEXT NOT NULL UNIQUE,
	expires_at  INTEGER NOT NULL
)`

// ClaimLink: the registration a claim link is for, as it was when the link
// was made (its id, name and certificate).
type ClaimLink struct {
	User     uint32
	Name     string
	CertHash string
}

func recoveryHash(token string) string {
	h := sha256.Sum256([]byte("recovery:" + token))
	return hex.EncodeToString(h[:])
}

// SetRecoveryLink gives an account a new link (an older one stops working).
func (s *Store) SetRecoveryLink(accountID int64, token string, expires time.Time) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO recovery_links (account_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		accountID, recoveryHash(token), expires.UnixMilli())
	return err
}

// DeleteRecoveryLinks ends all open links (roles changed).
func (s *Store) DeleteRecoveryLinks() error {
	_, err := s.db.Exec(`DELETE FROM recovery_links; DELETE FROM claim_links`)
	return err
}

// SetClaimLink gives a registration a new claim link (an older one stops
// working).
func (s *Store) SetClaimLink(l ClaimLink, token string, expires time.Time) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO claim_links (mumble_user, name, cert_hash, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		l.User, l.Name, l.CertHash, recoveryHash(token), expires.UnixMilli())
	return err
}

// ClaimLinkOf is a valid claim link's registration; else ErrNotFound.
func (s *Store) ClaimLinkOf(token string, now time.Time) (ClaimLink, error) {
	var l ClaimLink
	err := s.db.QueryRow(`SELECT mumble_user, name, cert_hash FROM claim_links WHERE token_hash = ? AND expires_at > ?`,
		recoveryHash(token), now.UnixMilli()).Scan(&l.User, &l.Name, &l.CertHash)
	if err == sql.ErrNoRows {
		return l, ErrNotFound
	}
	return l, err
}

// Claim uses a valid claim link once: it makes the account (an admin's
// link: no invitation needed) and move gives it the registration, in one
// transaction. ErrNotFound: the link is gone; ErrNameTaken: an account has
// the name now.
func (s *Store) Claim(token string, a Account, now time.Time, move func(*sql.Tx, ClaimLink) error) (Account, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	var l ClaimLink
	err = tx.QueryRow(`DELETE FROM claim_links WHERE token_hash = ? AND expires_at > ? RETURNING mumble_user, name, cert_hash`,
		recoveryHash(token), now.UnixMilli()).Scan(&l.User, &l.Name, &l.CertHash)
	if err == sql.ErrNoRows {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	if a, err = createAccount(tx, a, now); err != nil {
		return Account{}, err
	}
	if err := move(tx, l); err != nil {
		return Account{}, err
	}
	return a, tx.Commit()
}

// RecoveryAccount is the account of a valid link; else ErrNotFound.
func (s *Store) RecoveryAccount(token string, now time.Time) (Account, error) {
	return scanAccount(s.db.QueryRow(`SELECT `+accountColumns+` FROM recovery_links r JOIN accounts a ON a.id = r.account_id
		WHERE r.token_hash = ? AND r.expires_at > ?`, recoveryHash(token), now.UnixMilli()))
}

// Recover uses a valid link of the account once: the new password, and
// all its sessions end. ErrNotFound: the link is gone (used, replaced or
// expired meanwhile).
func (s *Store) Recover(token string, accountID int64, newHash string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := rowChanged(tx.Exec(`DELETE FROM recovery_links WHERE token_hash = ? AND account_id = ? AND expires_at > ?`,
		recoveryHash(token), accountID, now.UnixMilli())); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE accounts SET password_hash = ? WHERE id = ?`, newHash, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}
