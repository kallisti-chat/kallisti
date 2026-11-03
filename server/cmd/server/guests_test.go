package main

import (
	"errors"
	"slices"
	"testing"
	"time"

	"kallisti.chat/server/internal/certs"
	"kallisti.chat/server/internal/store"
)

func TestCleanGuests(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	add := func(name string, temporary bool, at time.Time) store.Account {
		certPEM, keyPEM, _ := certs.Generate(name)
		a, err := db.CreateAccount(store.Account{Name: name, CertPEM: certPEM, KeyPEM: keyPEM, Temporary: temporary}, at, "")
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	old := add("old", true, now.Add(-48*time.Hour))
	online := add("online", true, now.Add(-48*time.Hour))
	stuck := add("stuck", true, now.Add(-48*time.Hour))
	add("fresh", true, now.Add(-time.Hour))
	add("member", false, now.Add(-48*time.Hour))
	var removed []string
	unregister := func(hash string) (bool, error) {
		switch hash {
		case accountCertHash(stuck):
			return false, errors.New("database locked")
		case accountCertHash(online):
			return false, nil
		}
		removed = append(removed, hash)
		return true, nil
	}
	cleanGuests(db, now, unregister)
	if !slices.Equal(removed, []string{accountCertHash(old)}) {
		t.Fatalf("unregistered %v", removed)
	}
	for name, want := range map[string]bool{"old": false, "online": true, "stuck": true, "fresh": true, "member": true} {
		if _, err := db.AccountByName(name); (err == nil) != want {
			t.Errorf("%s: kept %v", name, err == nil)
		}
	}
	// Online counts as seen: a day later it is still there.
	if idle, _ := db.IdleTemporaryAccounts(now.Add(-time.Hour)); slices.ContainsFunc(idle, func(a store.Account) bool { return a.ID == online.ID }) {
		t.Fatal("online account not touched")
	}
}
