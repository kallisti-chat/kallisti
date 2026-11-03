package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/store"
)

// murmurFile writes the murmur package's test database to a file; cert:
// server 1 gets its own certificate and key.
func murmurFile(t *testing.T, cert bool) string {
	t.Helper()
	script, err := os.ReadFile("../../internal/murmur/testdata/murmur.sql")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "murmur.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	if cert {
		c, k := selfSigned(t)
		if _, err := db.Exec(`INSERT INTO config VALUES (1, 'certificate', ?), (1, 'key', ?)`, c, k); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func selfSigned(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "murmur"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}))
}

// kallisti is a server's environment in a temporary folder.
func kallisti(t *testing.T) (dir string, env func(string) string) {
	dir = t.TempDir()
	vars := map[string]string{"DB_PATH": filepath.Join(dir, "kallisti.db"), "ALLOWED_ORIGINS": "http://localhost", "BACKUP_DIR": filepath.Join(dir, "backups")}
	return dir, func(k string) string { return vars[k] }
}

func mumbleState(t *testing.T, dir string) (*mumbled.DB, func()) {
	t.Helper()
	db, err := store.Open(filepath.Join(dir, "kallisti.db"))
	if err != nil {
		t.Fatal(err)
	}
	mdb, err := mumbled.OpenDB(db.DB())
	if err != nil {
		t.Fatal(err)
	}
	return mdb, func() { db.Close() }
}

func TestImportMurmurAllServers(t *testing.T) {
	dir, env := kallisti(t)
	from := murmurFile(t, false)
	var out bytes.Buffer
	if err := importMurmur([]string{"-from", from, "-dry-run"}, env, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Bob is Bob-2") || !strings.Contains(out.String(), "dry run") {
		t.Errorf("dry run said %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "kallisti.db")); err == nil {
		t.Error("a dry run wrote")
	}
	out.Reset()
	if err := importMurmur([]string{"-from", from}, env, &out); err != nil {
		t.Fatal(err)
	}
	mdb, done := mumbleState(t, dir)
	defer done()
	users, _ := mdb.Users()
	chans, _ := mdb.Channels()
	if len(users) != 4 || len(chans) != 7 { // alice, bob, Bob-2, dave; root, 2 servers, 4 channels
		t.Fatalf("%d users, %d channels: %s", len(users), len(chans), out.String())
	}
	// Again: the server is not fresh any more.
	if err := importMurmur([]string{"-from", from}, env, &out); err == nil {
		t.Error("imported twice")
	}
}

func TestImportMurmurOneServerWithCert(t *testing.T) {
	dir, env := kallisti(t)
	var out bytes.Buffer
	if err := importMurmur([]string{"-from", murmurFile(t, true), "-server", "1", "-cert"}, env, &out); err != nil {
		t.Fatal(err)
	}
	mdb, done := mumbleState(t, dir)
	defer done()
	if name, _ := mdb.ServerName(); name != "Main" {
		t.Errorf("name %q", name)
	}
	key, err := os.Stat(filepath.Join(dir, "key.pem"))
	if err != nil || key.Mode().Perm() != 0o600 {
		t.Fatalf("key %v %v", key, err)
	}
	if !strings.Contains(out.String(), "certificate") {
		t.Errorf("said %q", out.String())
	}
}

func TestImportMurmurRefusesAWrongCert(t *testing.T) {
	_, env := kallisti(t)
	var out bytes.Buffer
	// Server 2 has no certificate of its own (Murmur's ini file has it).
	if err := importMurmur([]string{"-from", murmurFile(t, false), "-server", "2", "-cert"}, env, &out); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("err %v", err)
	}
}
