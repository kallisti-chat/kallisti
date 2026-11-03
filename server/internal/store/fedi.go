package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"kallisti.chat/server/internal/names"
)

// Fediverse logins (see auth's fedi.go): the app registered on each
// instance, and the remote accounts linked to accounts, by the instance's
// host and its id for the remote account (names can change there).
const fediTables = `CREATE TABLE IF NOT EXISTS fedi_apps (
	host          TEXT PRIMARY KEY,
	redirect_uri  TEXT NOT NULL,
	scopes        TEXT NOT NULL,
	client_id     TEXT NOT NULL,
	client_secret TEXT NOT NULL,
	created_at    INTEGER NOT NULL,
	used          INTEGER NOT NULL DEFAULT 0 -- a login went through
);
CREATE TABLE IF NOT EXISTS fedi_identities (
	host       TEXT NOT NULL,
	remote_id  TEXT NOT NULL,
	account_id INTEGER NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
	handle     TEXT NOT NULL,              -- @user@host at the last login
	public     INTEGER NOT NULL DEFAULT 0, -- shown on the profile
	created_at INTEGER NOT NULL,
	PRIMARY KEY (host, remote_id)
)`

var (
	// The remote account is linked to another account, or the account has
	// one already.
	ErrFediTaken = errors.New("fediverse account already linked")
	// The account would have no way left to log in.
	ErrLastLogin = errors.New("last way to log in")
)

type FediApp struct {
	Host, RedirectURI, Scopes, ClientID, ClientSecret string
	// Used: a login went through (instances may delete apps without one).
	Used      bool
	CreatedAt time.Time
}

type FediIdentity struct {
	Host, RemoteID, Handle string
	Public                 bool
}

// FediApp is the app registered on an instance for this redirect URI.
func (s *Store) FediApp(host, redirectURI string) (FediApp, error) {
	a := FediApp{Host: host, RedirectURI: redirectURI}
	var at int64
	err := s.db.QueryRow(`SELECT scopes, client_id, client_secret, used, created_at FROM fedi_apps WHERE host = ? AND redirect_uri = ?`,
		host, redirectURI).Scan(&a.Scopes, &a.ClientID, &a.ClientSecret, &a.Used, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return FediApp{}, ErrNotFound
	}
	a.CreatedAt = time.UnixMilli(at).UTC()
	return a, err
}

func (s *Store) MarkFediAppUsed(host string) error {
	_, err := s.db.Exec(`UPDATE fedi_apps SET used = 1 WHERE host = ?`, host)
	return err
}

// ForgetFediApp drops an instance's app: the next login registers again.
func (s *Store) ForgetFediApp(host string) error {
	_, err := s.db.Exec(`DELETE FROM fedi_apps WHERE host = ?`, host)
	return err
}

// SaveFediApp keeps an instance's app (replacing an older one).
func (s *Store) SaveFediApp(a FediApp, at time.Time) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO fedi_apps (host, redirect_uri, scopes, client_id, client_secret, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, a.Host, a.RedirectURI, a.Scopes, a.ClientID, a.ClientSecret, at.UnixMilli())
	return err
}

func linkFedi(tx *sql.Tx, accountID int64, id FediIdentity, at time.Time) error {
	_, err := tx.Exec(`INSERT INTO fedi_identities (host, remote_id, account_id, handle, created_at) VALUES (?, ?, ?, ?, ?)`,
		id.Host, id.RemoteID, accountID, id.Handle, at.UnixMilli())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrFediTaken
	}
	return err
}

// CreateFediAccount stores a new account (without a password) with its
// remote account, if the registration policy admits it (see admit).
func (s *Store) CreateFediAccount(a Account, id FediIdentity, at time.Time, invite string) (Account, error) {
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
	if err := linkFedi(tx, a.ID, id, at); err != nil {
		return Account{}, err
	}
	return a, tx.Commit()
}

// ErrSessionGone: the session that started a change ended meanwhile.
var ErrSessionGone = errors.New("the session ended")

// LinkFedi links a remote account to an account, while the session that
// asked for it (by its stored key) still lasts; a guest account becomes a
// normal one (it can log in again now).
func (s *Store) LinkFedi(accountID int64, session string, id FediIdentity, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND account_id = ? AND expires_at > ?`, session, accountID, at.UnixMilli()).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrSessionGone
	}
	if err := linkFedi(tx, accountID, id, at); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE accounts SET temporary = 0 WHERE id = ?`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}

// UnlinkFedi removes an account's remote account, but not its last way to
// log in (ErrLastLogin: it has no password).
func (s *Store) UnlinkFedi(accountID int64) error {
	res, err := s.db.Exec(`DELETE FROM fedi_identities WHERE account_id = ?
		AND EXISTS (SELECT 1 FROM accounts WHERE id = ? AND password_hash != '')`, accountID, accountID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	if _, err := s.FediIdentityOf(accountID); err != nil {
		return err
	}
	return ErrLastLogin
}

const identityColumns = `i.host, i.remote_id, i.handle, i.public`

func scanIdentity(row interface{ Scan(...any) error }) (FediIdentity, error) {
	var id FediIdentity
	err := row.Scan(&id.Host, &id.RemoteID, &id.Handle, &id.Public)
	if errors.Is(err, sql.ErrNoRows) {
		return FediIdentity{}, ErrNotFound
	}
	return id, err
}

// AccountByFedi is the account a remote account is linked to.
func (s *Store) AccountByFedi(host, remoteID string) (Account, FediIdentity, error) {
	id, err := scanIdentity(s.db.QueryRow(`SELECT `+identityColumns+` FROM fedi_identities i WHERE i.host = ? AND i.remote_id = ?`, host, remoteID))
	if err != nil {
		return Account{}, FediIdentity{}, err
	}
	a, err := scanAccount(s.db.QueryRow(`SELECT `+accountColumns+` FROM accounts a JOIN fedi_identities i ON i.account_id = a.id
		WHERE i.host = ? AND i.remote_id = ?`, host, remoteID))
	return a, id, err
}

// FediOnly is an account that logs in only with its remote account (no
// password).
type FediOnly struct {
	AccountID int64
	Name      string
	Identity  FediIdentity
}

// FediOnlyAccounts are all such accounts.
func (s *Store) FediOnlyAccounts() ([]FediOnly, error) {
	rows, err := s.db.Query(`SELECT i.account_id, a.name, ` + identityColumns + ` FROM fedi_identities i JOIN accounts a ON a.id = i.account_id
		WHERE a.password_hash = ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FediOnly
	for rows.Next() {
		var f FediOnly
		if err := rows.Scan(&f.AccountID, &f.Name, &f.Identity.Host, &f.Identity.RemoteID, &f.Identity.Handle, &f.Identity.Public); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FediIdentityOf is an account's remote account (ErrNotFound: none).
func (s *Store) FediIdentityOf(accountID int64) (FediIdentity, error) {
	return scanIdentity(s.db.QueryRow(`SELECT `+identityColumns+` FROM fedi_identities i WHERE i.account_id = ?`, accountID))
}

// UpdateFediHandle keeps the handle current (renames on the instance).
func (s *Store) UpdateFediHandle(host, remoteID, handle string) error {
	_, err := s.db.Exec(`UPDATE fedi_identities SET handle = ? WHERE host = ? AND remote_id = ?`, handle, host, remoteID)
	return err
}

// SetFediPublic shows or hides the handle on the account's profile.
func (s *Store) SetFediPublic(accountID int64, on bool) error {
	res, err := s.db.Exec(`UPDATE fedi_identities SET public = ? WHERE account_id = ?`, on, accountID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return ErrNotFound
}

// PublicFediHandle is the handle an account shows on its profile ("": none).
func (s *Store) PublicFediHandle(name string) (string, error) {
	var h string
	err := s.db.QueryRow(`SELECT i.handle FROM fedi_identities i JOIN accounts a ON a.id = i.account_id
		WHERE a.name_key = ? AND i.public = 1`, names.Key(name)).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return h, err
}
