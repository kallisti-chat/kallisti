package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestBackupMakesAReadableCopyAndKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec(`CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('hello')`)
	out := filepath.Join(dir, "backups")
	day := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	for i := range 5 {
		if _, err := Run(db, out, 3, day.AddDate(0, 0, i)); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(out, "kallisti-*.db"))
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	slices.Sort(names)
	want := []string{"kallisti-20261003-030000.db", "kallisti-20261004-030000.db", "kallisti-20261005-030000.db"}
	if !slices.Equal(names, want) {
		t.Fatalf("backups %v", names)
	}
	copy, _ := sql.Open("sqlite", files[len(files)-1])
	defer copy.Close()
	var v string
	if err := copy.QueryRow(`SELECT v FROM t`).Scan(&v); err != nil || v != "hello" {
		t.Fatalf("copy: %q %v", v, err)
	}
	// Other files in the folder are left alone.
	os.WriteFile(filepath.Join(out, "notes.txt"), nil, 0o600)
	Run(db, out, 1, day.AddDate(0, 0, 9))
	if _, err := os.Stat(filepath.Join(out, "notes.txt")); err != nil {
		t.Fatal("removed a file that is not a backup")
	}
}

func TestBeforeStartCopiesTheDatabaseAsItIs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "k.db")
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	// No database yet (a new server): nothing to copy.
	if path, err := BeforeStart(dbPath, filepath.Join(dir, "backups"), 3, now); path != "" || err != nil {
		t.Fatalf("new server: %q %v", path, err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE old (x)`)
	db.Exec(`INSERT INTO old VALUES (42)`)
	db.Close()
	path, err := BeforeStart(dbPath, filepath.Join(dir, "backups"), 3, now)
	if err != nil || path == "" {
		t.Fatal(path, err)
	}
	cp, _ := sql.Open("sqlite", path)
	defer cp.Close()
	var x int
	if err := cp.QueryRow(`SELECT x FROM old`).Scan(&x); err != nil || x != 42 {
		t.Fatalf("copy: %d %v", x, err)
	}
}

// The daily copy must not wait for the server's one connection: here that
// connection is taken, in a write that is not committed yet. The copy
// still finishes, and has what was committed before.
func TestFromFileDoesNotUseTheServersConnection(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "k.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('hello')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE; INSERT INTO t VALUES ('not yet')`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	var path string
	go func() {
		var err error
		path, err = FromFile(dbPath, filepath.Join(dir, "backups"), 3, time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the copy waits for the server's connection")
	}
	// The copy did not end the server's write.
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		t.Fatal(err)
	}
	cp, _ := sql.Open("sqlite", path)
	defer cp.Close()
	rows, err := cp.Query(`SELECT v FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var v string
		rows.Scan(&v)
		got = append(got, v)
	}
	if !slices.Equal(got, []string{"hello"}) {
		t.Fatalf("copy: %q", got)
	}
}

// A database file that went missing while the server runs: an error, and
// no new empty database in its place.
func TestFromFileDoesNotCreateTheDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gone.db")
	if path, err := FromFile(dbPath, filepath.Join(dir, "backups"), 3, time.Now()); err == nil {
		t.Fatalf("a copy of nothing: %q", path)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("the database was created: %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "backups", pattern)); len(files) != 0 {
		t.Fatalf("backups %v", files)
	}
}

// A database in WAL mode that was closed cleanly has no -wal and -shm
// files: the copy before the start must still open it read-only.
func TestBeforeStartCopiesAClosedWALDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "k.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('hello')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	path, err := BeforeStart(dbPath, filepath.Join(dir, "backups"), 3, time.Now())
	if err != nil || path == "" {
		t.Fatal(path, err)
	}
	cp, _ := sql.Open("sqlite", path)
	defer cp.Close()
	var v string
	if err := cp.QueryRow(`SELECT v FROM t`).Scan(&v); err != nil || v != "hello" {
		t.Fatalf("copy: %q %v", v, err)
	}
}
