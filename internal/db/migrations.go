package db

import (
	"bytes"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// noTxMarker, when present as the first line of a migration file, opts
// the migration out of the default per-file transaction wrapping. The
// migration body is then responsible for its own atomicity (and may
// issue PRAGMA statements that require running outside a transaction,
// e.g. PRAGMA foreign_keys=OFF for table-recreation recipes).
var noTxMarker = []byte("-- @no-tx")

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all SQL files in migrations/ in lexical order. Each file
// is applied at most once; the schema_migrations table records what has run.
// Files are run inside a transaction so partial application cannot corrupt
// the database.
func Migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	applied := map[string]bool{}
	rows, err := d.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query schema_migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	for _, name := range names {
		if applied[name] {
			continue
		}
		sqlBytes, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if bytes.HasPrefix(bytes.TrimSpace(sqlBytes), noTxMarker) {
			if _, err := d.Exec(string(sqlBytes)); err != nil {
				return fmt.Errorf("apply %s: %w", name, err)
			}
			// In no-tx mode the migration is responsible for inserting its
			// own schema_migrations row inside whatever transactional
			// boundary it set up. We only insert here as a safety net for
			// migrations that forgot — INSERT OR IGNORE means re-runs of
			// fresh DBs that already self-recorded won't double-fail.
			if _, err := d.Exec(
				`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (?, strftime('%s','now'))`,
				name,
			); err != nil {
				return fmt.Errorf("record %s: %w", name, err)
			}
			continue
		}
		tx, err := d.Begin()
		if err != nil {
			return fmt.Errorf("begin %s: %w", name, err)
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, strftime('%s','now'))`,
			name,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}
