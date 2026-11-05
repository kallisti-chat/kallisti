package store

import "crypto/rand"

// Secret is a random key of 32 bytes by name, made on first use (file_links:
// see filelink).
func (s *Store) Secret(name string) ([]byte, error) {
	key := make([]byte, 32)
	rand.Read(key)
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO secrets (name, value) VALUES (?, ?)`, name, key); err != nil {
		return nil, err
	}
	err := s.db.QueryRow(`SELECT value FROM secrets WHERE name = ?`, name).Scan(&key)
	return key, err
}
