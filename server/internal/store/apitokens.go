package store

import (
	"errors"
	"time"
)

// Personal access tokens: programs (agents, scripts) read and write as an
// account over the agent API. Stored by the hash of the token, like
// sessions; write: may post, else read only.
const apiTokensTable = `CREATE TABLE IF NOT EXISTS api_tokens (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	token_hash TEXT NOT NULL UNIQUE,
	write      INTEGER NOT NULL,
	created_at INTEGER NOT NULL
)`

const maxAPITokens = 20

var ErrTooManyTokens = errors.New("too many tokens")

type APIToken struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Write   bool      `json:"write"`
	Created time.Time `json:"created"`
}

// CreateAPIToken: at most maxAPITokens per account (ErrTooManyTokens).
func (s *Store) CreateAPIToken(accountID int64, name, tokenHash string, write bool, now time.Time) (APIToken, error) {
	res, err := s.db.Exec(`INSERT INTO api_tokens (account_id, name, token_hash, write, created_at)
		SELECT ?, ?, ?, ?, ? WHERE (SELECT count(*) FROM api_tokens WHERE account_id = ?) < ?`,
		accountID, name, tokenHash, write, now.UnixMilli(), accountID, maxAPITokens)
	if err != nil {
		return APIToken{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return APIToken{}, ErrTooManyTokens
	}
	id, err := res.LastInsertId()
	return APIToken{ID: id, Name: name, Write: write, Created: now.UTC()}, err
}

// APITokens are an account's tokens, oldest first.
func (s *Store) APITokens(accountID int64) ([]APIToken, error) {
	rows, err := s.db.Query(`SELECT id, name, write, created_at FROM api_tokens WHERE account_id = ? ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		var at int64
		if err := rows.Scan(&t.ID, &t.Name, &t.Write, &at); err != nil {
			return nil, err
		}
		t.Created = time.UnixMilli(at).UTC()
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken removes one of the account's tokens (else ErrNotFound).
func (s *Store) DeleteAPIToken(accountID, id int64) error {
	return rowChanged(s.db.Exec(`DELETE FROM api_tokens WHERE id = ? AND account_id = ?`, id, accountID))
}

// DeleteAPITokens ends all of an account's tokens.
func (s *Store) DeleteAPITokens(accountID int64) error {
	_, err := s.db.Exec(`DELETE FROM api_tokens WHERE account_id = ?`, accountID)
	return err
}

// AccountByAPIToken is a token's account and whether it may write.
func (s *Store) AccountByAPIToken(tokenHash string) (Account, bool, error) {
	var write bool
	if err := s.db.QueryRow(`SELECT write FROM api_tokens WHERE token_hash = ?`, tokenHash).Scan(&write); err != nil {
		return Account{}, false, notFound(err)
	}
	acc, err := scanAccount(s.db.QueryRow(`SELECT `+accountColumns+` FROM api_tokens t JOIN accounts a ON a.id = t.account_id
		WHERE t.token_hash = ?`, tokenHash))
	return acc, write, err
}
