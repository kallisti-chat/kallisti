// Package backup makes consistent copies of the SQLite database, before
// the server starts (and changes the database: migrations are one-way) and
// daily while it runs, and keeps the newest few.
package backup

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	_ "modernc.org/sqlite"
)

const pattern = "kallisti-*.db"

// Run writes a copy named after the time into dir (VACUUM INTO: a
// consistent snapshot) and removes all but the newest keep copies.
func Run(db *sql.DB, dir string, keep int, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "kallisti-"+now.UTC().Format("20060102-150405")+".db")
	if _, err := db.Exec(`VACUUM INTO ?`, path); err != nil {
		return "", fmt.Errorf("backup to %s: %w", path, err)
	}
	old, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return path, err
	}
	slices.Sort(old) // the names sort by time
	for _, f := range old[:max(0, len(old)-keep)] {
		if err := os.Remove(f); err != nil {
			slog.Warn("remove old backup", "file", f, "err", err)
		}
	}
	return path, nil
}

// BeforeStart copies the database as it is, before the server opens it
// (an update may migrate it). No database yet (a new server): nothing.
func BeforeStart(dbPath, dir string, keep int, now time.Time) (string, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return FromFile(dbPath, dir, keep, now)
}

// FromFile is Run on a connection of its own. The server has one
// connection for everything (see store.Open), and a copy on it would hold
// up every message and login until it is done. A second connection only
// reads (the database is in WAL mode), so the server goes on meanwhile.
func FromFile(dbPath, dir string, keep int, now time.Time) (string, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	defer db.Close()
	return Run(db, dir, keep, now)
}

// Daily runs a backup every day (the first a day after the start, which
// had its own: see BeforeStart), until the process ends.
func Daily(dbPath, dir string, keep int) {
	for {
		time.Sleep(24 * time.Hour)
		if path, err := FromFile(dbPath, dir, keep, time.Now()); err != nil {
			slog.Error("database backup", "err", err)
		} else {
			slog.Info("database backup", "file", path)
		}
	}
}
