// Package settings is a thin key-value store backed by the `settings` table.
// It also bootstraps the JWT secret used to sign session cookies.
package settings

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
)

// Reserved keys used by the rest of the codebase.
const (
	KeyJWTSecret           = "jwt_secret"
	KeyGridCols            = "grid_cols"
	KeyHealthcheckInterval = "healthcheck_interval"
)

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

// Get returns the string value for key or "" if the key is not present.
func (s *Store) Get(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Set upserts the (key, value) pair.
func (s *Store) Set(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

// GetInt parses the stored value as an integer, returning def when the key
// is unset. Returns an error if the value cannot be parsed.
func (s *Store) GetInt(key string, def int) (int, error) {
	v, err := s.Get(key)
	if err != nil {
		return 0, err
	}
	if v == "" {
		return def, nil
	}
	return strconv.Atoi(v)
}

// All returns all settings as a map. The jwt_secret key is excluded from
// the result so it never leaks through an API response.
func (s *Store) All() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings WHERE key != ?`, KeyJWTSecret)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// GetOrCreateJWTSecret returns the stored JWT signing secret, generating
// a fresh 32-byte random secret (hex encoded) and persisting it on first
// call. The returned hex string is stable for the life of the DB file.
func (s *Store) GetOrCreateJWTSecret() (string, error) {
	v, err := s.Get(KeyJWTSecret)
	if err != nil {
		return "", err
	}
	if v != "" {
		return v, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	v = hex.EncodeToString(b)
	if err := s.Set(KeyJWTSecret, v); err != nil {
		return "", err
	}
	return v, nil
}
