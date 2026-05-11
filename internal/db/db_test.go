package db

import (
	"testing"
)

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	if err := Migrate(d); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(d); err != nil {
		t.Fatalf("second migrate (should be no-op): %v", err)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if n == 0 {
		t.Error("expected at least one migration recorded")
	}
}

func TestMigrateSeedsDefaultTheme(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}

	var name string
	var isDefault, isActive int
	row := d.QueryRow(`SELECT name, is_default, is_active FROM themes WHERE is_default=1`)
	if err := row.Scan(&name, &isDefault, &isActive); err != nil {
		t.Fatalf("default theme not seeded: %v", err)
	}
	if isDefault != 1 || isActive != 1 {
		t.Errorf("seed flags: is_default=%d is_active=%d", isDefault, isActive)
	}
	if name == "" {
		t.Error("default theme has empty name")
	}
}

func TestSchemaHasExpectedTables(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}
	want := []string{"users", "categories", "services", "themes", "settings"}
	for _, table := range want {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if n != 1 {
			t.Errorf("table %s missing", table)
		}
	}
}
