package store

import (
	"crypto/sha256"
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
)`

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
	_, err := s.db.Exec(`DELETE FROM recovery_links`)
	return err
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
