// Package db wraps SQLite (modernc.org/sqlite — pure-Go, CGO-free) with the
// PRAGMAs we want everywhere: WAL journal, busy_timeout, foreign_keys ON,
// synchronous=NORMAL. Migrations live in ./migrations and run via goose.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the embedded migration filesystem rooted at "migrations".
// TODO(phase 2): wire into goose.Provider in migrate.go.
func Migrations() fs.FS {
	sub, _ := fs.Sub(migrationsFS, "migrations")
	return sub
}

// Open opens (and pings) the SQLite DB at the given path with sane PRAGMAs.
// Path conventions:
//   - leader → "${DATA_DIR}/labextend.db"
//   - tests → ":memory:" or a temp file
func Open(ctx context.Context, dbPath string) (*sql.DB, error) {
	// modernc.org/sqlite accepts a DSN that includes _pragma options.
	// We add them as query params so they're applied to *every* connection
	// in the pool — important because Pragmas are per-connection in SQLite.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_pragma=synchronous(NORMAL)",
		filepath.ToSlash(dbPath),
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	// SQLite is single-writer; cap connections accordingly. Reads can scale
	// because WAL allows readers concurrent with one writer.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return db, nil
}

// Migrate is implemented in migrate.go (Phase 2 complete).
