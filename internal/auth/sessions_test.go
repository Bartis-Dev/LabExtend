package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/db"
)

func TestSessionIdleTimeout(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, is_admin, created_at, updated_at) VALUES (1, 'a@x', 0, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	store := NewSessionStore(d, "bpm_session", 12*time.Hour, 2*time.Hour, false)

	newSession := func() *http.Cookie {
		rec := httptest.NewRecorder()
		if _, err := store.Create(ctx, rec, httptest.NewRequest(http.MethodGet, "/", nil), 1); err != nil {
			t.Fatal(err)
		}
		return rec.Result().Cookies()[0]
	}
	get := func(c *http.Cookie) *Session {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(c)
		s, err := store.Get(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	active := newSession()
	if _, err := d.Exec(`UPDATE sessions SET last_seen = ? WHERE id = ?`, time.Now().Add(-time.Hour).Unix(), active.Value); err != nil {
		t.Fatal(err)
	}
	if get(active) == nil {
		t.Fatal("a session used an hour ago must still be valid")
	}

	idle := newSession()
	if _, err := d.Exec(`UPDATE sessions SET last_seen = ? WHERE id = ?`, time.Now().Add(-3*time.Hour).Unix(), idle.Value); err != nil {
		t.Fatal(err)
	}
	if get(idle) != nil {
		t.Fatal("a session idle for 3h must be rejected with a 2h idle timeout")
	}

	stale := newSession()
	if _, err := d.Exec(`UPDATE sessions SET last_seen = ? WHERE id = ?`, time.Now().Add(-3*time.Hour).Unix(), stale.Value); err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeExpired(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purge should leave only the active session, %d left", n)
	}
}
