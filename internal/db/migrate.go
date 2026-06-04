package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

// Migrate brings the database up to the latest schema version using the
// embedded migrations under ./migrations. Safe to call on every boot —
// goose tracks applied versions in `goose_db_version`.
func Migrate(ctx context.Context, sqlDB *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetTableName("goose_db_version")

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose set dialect: %w", err)
	}

	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// Version returns the current schema version applied to the DB.
// Returns 0 if no migrations have been applied yet.
func Version(ctx context.Context, sqlDB *sql.DB) (int64, error) {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return 0, err
	}
	v, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return 0, fmt.Errorf("goose version: %w", err)
	}
	return v, nil
}
