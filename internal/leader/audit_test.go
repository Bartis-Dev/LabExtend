package leader

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Bartis-Dev/LabExtend/internal/db"
)

func auditDB(t *testing.T, users, rows int) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < users; i++ {
		if _, err := d.Exec(`INSERT INTO users (email, created_at, updated_at) VALUES (?, 0, 0)`,
			"u"+strconv.Itoa(i)+"@x"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < rows; i++ {
		if _, err := d.Exec(`INSERT INTO audit_log (ts, action) VALUES (?, 'old')`, i); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func auditRows(t *testing.T, d *sql.DB) (n int, minID int64) {
	t.Helper()
	if err := d.QueryRow(`SELECT COUNT(*), COALESCE(MIN(id),0) FROM audit_log`).Scan(&n, &minID); err != nil {
		t.Fatal(err)
	}
	return n, minID
}

func TestAuditAutoSingleUserKeepsNewest100(t *testing.T) {
	d := auditDB(t, 1, 250)
	a := newAuditLogger(d)
	ctx := context.Background()
	// The one-time effect in production: startup trims the existing log.
	if err := a.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if n, minID := auditRows(t, d); n != 100 || minID != 151 {
		t.Fatalf("after load: %d rows from id %d, want 100 from 151", n, minID)
	}
	a.Log(ctx, nil, "new", "", "", nil)
	if n, minID := auditRows(t, d); n != 100 || minID != 152 {
		t.Fatalf("after insert: %d rows from id %d, want 100 from 152", n, minID)
	}
	if st := a.State(ctx); !st.Enabled || st.Keep != 100 || st.Users != 1 {
		t.Fatalf("state %+v", st)
	}
}

func TestAuditAutoTwoUsersKeepsAll(t *testing.T) {
	d := auditDB(t, 2, 150)
	a := newAuditLogger(d)
	if err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.Log(context.Background(), nil, "new", "", "", nil)
	if n, _ := auditRows(t, d); n != 151 {
		t.Fatalf("rows = %d, want 151", n)
	}
}

func TestAuditModesOnAndOff(t *testing.T) {
	d := auditDB(t, 1, 150)
	a := newAuditLogger(d)
	ctx := context.Background()
	if err := a.SetMode(ctx, auditModeOn); err != nil {
		t.Fatal(err)
	}
	a.Log(ctx, nil, "new", "", "", nil)
	if n, _ := auditRows(t, d); n != 151 {
		t.Fatalf("on: rows = %d, want 151", n)
	}
	if err := a.SetMode(ctx, auditModeOff); err != nil {
		t.Fatal(err)
	}
	a.Log(ctx, nil, "new", "", "", nil)
	if n, _ := auditRows(t, d); n != 151 {
		t.Fatalf("off: rows = %d, want 151", n)
	}
	if a.State(ctx).Enabled {
		t.Fatal("off reports enabled")
	}
	// The mode survives a restart.
	b := newAuditLogger(d)
	if err := b.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if b.State(ctx).Mode != auditModeOff {
		t.Fatalf("mode after reload = %q", b.State(ctx).Mode)
	}
	if err := a.SetMode(ctx, "loud"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestFileBrowserGate(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	requireFileBrowser(false)(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nodes/n/files", nil))
	if rec.Code != http.StatusForbidden || rec.Body.String() != "{\"error\":\"file browser is disabled (BPM_FILEBROWSER)\"}\n" {
		t.Fatalf("disabled: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	requireFileBrowser(true)(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nodes/n/files", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled: %d", rec.Code)
	}
}
