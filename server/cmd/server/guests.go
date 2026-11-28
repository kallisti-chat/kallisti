package main

import (
	"crypto/sha1" // #nosec G505 -- Mumble's certificate hash (see certHash)
	"encoding/hex"
	"encoding/pem"
	"log/slog"
	"time"

	"kallisti.chat/server/internal/store"
)

// Temporary (guest) accounts live this long after their last visit.
const guestLifetime = 24 * time.Hour

// cleanGuests deletes temporary accounts not seen for guestLifetime. Their
// Mumble registration goes first; unregister keeps it (gone false) while the
// user is online, and then the account counts as seen. The account is only
// deleted if it is still idle and temporary (no visit or password since).
func cleanGuests(db *store.Store, now time.Time, unregister func(certHash string) (gone bool, err error)) {
	before := now.Add(-guestLifetime)
	idle, err := db.IdleTemporaryAccounts(before)
	if err != nil {
		slog.Error("finding idle guest accounts", "err", err)
		return
	}
	for _, acc := range idle {
		gone, err := unregister(accountCertHash(acc))
		if err != nil {
			slog.Error("removing a guest's registration", "account", acc.Name, "err", err)
			continue
		}
		if !gone {
			db.TouchAccount(acc.ID, now) // online
			continue
		}
		if _, err := db.DeleteIdleTemporaryAccount(acc.ID, before); err != nil {
			slog.Error("deleting a guest account", "account", acc.Name, "err", err)
		}
	}
}

// accountCertHash is the Mumble certificate hash of an account (SHA-1 of
// the DER certificate, as Mumble's).
func accountCertHash(acc store.Account) string {
	block, _ := pem.Decode(acc.CertPEM)
	if block == nil {
		return ""
	}
	return certHash(block.Bytes)
}

func certHash(der []byte) string {
	sum := sha1.Sum(der) // #nosec G401 -- Mumble's certificate hash, as Murmur's
	return hex.EncodeToString(sum[:])
}
