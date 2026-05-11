package settings

import (
	"testing"

	"github.com/Bartis-Dev/LabExtend/internal/db"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	return New(d)
}

func TestGetSetRoundtrip(t *testing.T) {
	s := openTestDB(t)
	if v, _ := s.Get("missing"); v != "" {
		t.Errorf("missing key should be empty, got %q", v)
	}
	if err := s.Set("foo", "bar"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("foo"); v != "bar" {
		t.Errorf("after set: got %q want bar", v)
	}
	if err := s.Set("foo", "baz"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("foo"); v != "baz" {
		t.Errorf("after upsert: got %q want baz", v)
	}
}

func TestGetInt(t *testing.T) {
	s := openTestDB(t)
	n, err := s.GetInt("grid_cols", 6)
	if err != nil || n != 6 {
		t.Errorf("missing key default: n=%d err=%v", n, err)
	}
	_ = s.Set("grid_cols", "8")
	n, _ = s.GetInt("grid_cols", 6)
	if n != 8 {
		t.Errorf("got %d, want 8", n)
	}
}

func TestGetOrCreateJWTSecret(t *testing.T) {
	s := openTestDB(t)
	a, err := s.GetOrCreateJWTSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 {
		t.Errorf("hex length = %d, want 64", len(a))
	}
	b, _ := s.GetOrCreateJWTSecret()
	if a != b {
		t.Errorf("secret regenerated on second call: %q vs %q", a, b)
	}
}

func TestAllOmitsJWTSecret(t *testing.T) {
	s := openTestDB(t)
	_, _ = s.GetOrCreateJWTSecret()
	_ = s.Set("grid_cols", "6")
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := all[KeyJWTSecret]; ok {
		t.Error("jwt_secret leaked through All()")
	}
	if all["grid_cols"] != "6" {
		t.Errorf("missing public setting: %#v", all)
	}
}
