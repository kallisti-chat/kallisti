package main

import (
	"cmp"
	"crypto/tls"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kallisti.chat/server/internal/backup"
	"kallisti.chat/server/internal/config"
	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/murmur"
	"kallisti.chat/server/internal/store"
)

// importMurmur is `server import-murmur`: a Murmur server's channels,
// users, groups, ACLs and bans into a new Kallisti server (stopped; see
// docs/migrating-from-murmur.md).
func importMurmur(args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("import-murmur", flag.ContinueOnError)
	fs.SetOutput(out)
	from := fs.String("from", "", "the Murmur database (murmur.sqlite, mumble-server.sqlite)")
	only := fs.Int("server", 0, "only this virtual server, as the whole server (default: each virtual server as a channel; with only one, that one)")
	withCert := fs.Bool("cert", false, "use the Murmur server's certificate and key for Mumble clients (in CERT_DIR)")
	dry := fs.Bool("dry-run", false, "only show what would be imported")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" {
		return errors.New("-from: the Murmur database is needed")
	}
	cfg, err := config.Parse(getenv)
	if err != nil {
		return err
	}
	if cfg.Builtin == nil {
		return errors.New("MUMBLE_HOST is set: the import is for Kallisti's built-in Mumble server")
	}
	abs, err := filepath.Abs(*from)
	if err != nil {
		return err
	}
	src, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}).String())
	if err != nil {
		return err
	}
	defer src.Close()
	list, err := murmur.Servers(src)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errors.New("no virtual servers in " + *from)
	}
	ids := []int{*only}
	if *only == 0 {
		ids = ids[:0]
		for _, s := range list {
			ids = append(ids, s.ID)
		}
	}
	var servers []murmur.Server
	for _, id := range ids {
		s, err := murmur.Read(src, id)
		if err != nil {
			return err
		}
		servers = append(servers, s)
	}
	imp, rep := murmur.Plan(servers, len(servers) == 1)
	fmt.Fprintf(out, "Murmur: %s\n", *from)
	for _, s := range list {
		fmt.Fprintf(out, "  server %d %q: %d users, %d channels\n", s.ID, s.Name, s.Users, s.Channels)
	}
	how := "each server as a channel under root"
	if rep.Whole {
		how = fmt.Sprintf("server %d as the whole server", servers[0].ID)
	}
	fmt.Fprintf(out, "Import: %s; %d channels, %d users, %d bans\n", how, len(imp.Channels), len(imp.Users), len(imp.Bans))
	if notes := rep.Notes(); notes != "" {
		fmt.Fprintln(out, "  - "+strings.ReplaceAll(notes, "\n", "\n  - "))
	}
	var cert *murmur.Server
	if *withCert {
		if cfg.Builtin.CertFile != "" {
			return errors.New("-cert: MUMBLE_CERT_FILE is set, so the server would not use the imported certificate")
		}
		cert = &servers[0]
		fmt.Fprintf(out, "Certificate: server %d's\n", cert.ID)
		if cert.CertPEM == "" || cert.KeyPEM == "" {
			return fmt.Errorf("server %d has no certificate of its own (Murmur's ini file has it: set MUMBLE_CERT_FILE and MUMBLE_KEY_FILE to those files)", cert.ID)
		}
		if _, err := tls.X509KeyPair([]byte(cert.CertPEM), []byte(cert.KeyPEM)); err != nil {
			return fmt.Errorf("server %d's certificate and key: %w", cert.ID, err)
		}
	}
	if *dry {
		fmt.Fprintln(out, "This was a dry run: nothing written.")
		return nil
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database %s: %w", cfg.DBPath, err)
	}
	defer db.Close()
	// Names of web accounts are theirs only (see the server's ReservedName).
	var taken []string
	for _, u := range imp.Users {
		if _, err := db.AccountByName(u.Name); err == nil {
			taken = append(taken, u.Name)
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	if len(taken) > 0 {
		return fmt.Errorf("Kallisti accounts already have these names: %s", strings.Join(taken, ", "))
	}
	dir := cmp.Or(cfg.BackupDir, filepath.Join(filepath.Dir(cfg.DBPath), "backups"))
	if path, err := backup.Run(db.DB(), dir, max(cfg.BackupKeep, 1), time.Now()); err != nil {
		return fmt.Errorf("backup before the import: %w", err)
	} else {
		fmt.Fprintf(out, "Backup: %s\n", path)
	}
	mdb, err := mumbled.OpenDB(db.DB())
	if err != nil {
		return err
	}
	if err := mdb.Import(imp); err != nil {
		return err
	}
	fmt.Fprintln(out, "Imported.")
	if cert != nil {
		if err := installCert(cfg.Builtin.CertDir, cert.CertPEM, cert.KeyPEM, out); err != nil {
			return err
		}
	}
	return nil
}

// installCert writes the Mumble certificate and key (see
// certs.LoadOrCreateServer), the old ones kept beside them.
func installCert(dir, certPEM, keyPEM string, out io.Writer) error {
	stamp := time.Now().Format("20060102-150405")
	kept := false
	files := []struct {
		name, data string
		mode       os.FileMode
	}{{"cert.pem", certPEM, 0o644}, {"key.pem", keyPEM, 0o600}}
	// Both written first: a failure leaves the old pair.
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name+".new"), []byte(f.data), f.mode); err != nil {
			return err
		}
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if _, err := os.Stat(path); err == nil {
			if err := os.Rename(path, path+".before-murmur-import-"+stamp); err != nil {
				return err
			}
			kept = true
		}
		if err := os.Rename(path+".new", path); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Mumble certificate: the Murmur server's, in %s\n", dir)
	if kept {
		fmt.Fprintf(out, "  (the old one kept as *.before-murmur-import-%s)\n", stamp)
	}
	return nil
}
