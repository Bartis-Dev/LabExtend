// Package db opens the LabExtend SQLite database, applies migrations, and
// exposes the *sql.DB handle the rest of the app uses.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open ensures dataDir exists and returns a connected *sql.DB pointed at
// "<dataDir>/labextend.db". WAL is enabled and foreign keys are on.
// MaxOpenConns is pinned to 1 because SQLite only supports a single writer.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "labextend.db")
	// modernc.org/sqlite accepts <path>?_pragma=... — using a plain path
	// avoids URI-escape mishandling of leading slashes on POSIX systems.
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		dbPath)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := d.Ping(); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	d.SetMaxOpenConns(1)
	return d, nil
}
