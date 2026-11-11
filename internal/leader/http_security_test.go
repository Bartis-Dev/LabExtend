package leader

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/db"
)

type routerEnv struct {
	t      *testing.T
	db     *sql.DB
	h      http.Handler
	cookie map[bool]*http.Cookie // by is_admin
	csrf   map[bool]string
}

// newRouterEnv runs the real router with one admin and one plain user signed in.
func newRouterEnv(t *testing.T) *routerEnv {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/r.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	store := auth.NewSessionStore(d, "bpm_session", time.Hour, time.Hour, false)
	deps := &leaderDeps{
		DB: d, Registry: NewAgentRegistry(), Hub: NewHub(),
		Metrics: newMetricsStore(d), Containers: newContainerStore(d),
		Audit: newAuditLogger(d),
	}
	e := &routerEnv{t: t, db: d, h: newRouter(ctx, &config.Config{}, deps, store),
		cookie: map[bool]*http.Cookie{}, csrf: map[bool]string{}}
	for i, admin := range []bool{false, true} {
		res, err := d.Exec(`INSERT INTO users (email, is_admin, created_at, updated_at) VALUES (?, ?, 0, 0)`,
			[]string{"user@x", "admin@x"}[i], boolI(admin))
		if err != nil {
			t.Fatal(err)
		}
		uid, _ := res.LastInsertId()
		rec := httptest.NewRecorder()
		sess, err := store.Create(ctx, rec, httptest.NewRequest(http.MethodGet, "/", nil), uid)
		if err != nil {
			t.Fatal(err)
		}
		e.cookie[admin] = rec.Result().Cookies()[0]
		e.csrf[admin] = sess.CSRFToken
	}
	return e
}

func (e *routerEnv) do(admin bool, method, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(e.cookie[admin])
	req.Header.Set("X-CSRF-Token", e.csrf[admin])
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *routerEnv) auditCount(action string) int {
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = ?`, action).Scan(&n)
	return n
}

const validHook = `{"name":"d","url":"https://discord.com/api/webhooks/1/tok","enabled":true,"event_types":["node.offline"]}`

func TestWebhookAndCleanupRoutesAreAdminOnly(t *testing.T) {
	e := newRouterEnv(t)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/webhooks", validHook},
		{http.MethodPut, "/api/webhooks/x", validHook},
		{http.MethodDelete, "/api/webhooks/x", ""},
		{http.MethodPost, "/api/webhooks/x/test", ""},
		{http.MethodPost, "/api/nodes/cleanup", `{"all":true}`},
	} {
		if rec := e.do(false, c.method, c.path, c.body); rec.Code != http.StatusForbidden {
			t.Errorf("non-admin %s %s = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	if rec := e.do(false, http.MethodGet, "/api/webhooks", ""); rec.Code != http.StatusOK {
		t.Errorf("listing webhooks should stay open to users, got %d", rec.Code)
	}
}

func TestWebhookURLValidationAndAudit(t *testing.T) {
	e := newRouterEnv(t)
	bad := `{"name":"d","url":"http://169.254.169.254/latest","event_types":[]}`
	if rec := e.do(true, http.MethodPost, "/api/webhooks", bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-Discord URL on create = %d, want 400", rec.Code)
	}
	if rec := e.do(true, http.MethodPost, "/api/webhooks", validHook); rec.Code != http.StatusOK {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	var id string
	_ = e.db.QueryRow(`SELECT id FROM webhook_configs`).Scan(&id)
	if rec := e.do(true, http.MethodPut, "/api/webhooks/"+id, bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-Discord URL on update = %d, want 400", rec.Code)
	}
	masked := `{"name":"d2","url":"https://discor` + "…" + `/tok","enabled":true,"event_types":[]}`
	if rec := e.do(true, http.MethodPut, "/api/webhooks/"+id, masked); rec.Code != http.StatusOK {
		t.Fatalf("update keeping the masked URL = %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(true, http.MethodDelete, "/api/webhooks/"+id, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	for _, a := range []string{"webhook.create", "webhook.update", "webhook.delete"} {
		if n := e.auditCount(a); n != 1 {
			t.Errorf("want one %s audit row, got %d", a, n)
		}
	}
}

func TestCleanupNodesRequiresExplicitScope(t *testing.T) {
	e := newRouterEnv(t)
	if _, err := e.db.Exec(`INSERT INTO nodes (id, hostname, first_seen, last_seen) VALUES ('n1', 'h1', 0, 0)`); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"", "not json", `{}`, `{"older_than_hours":0}`, `{"older_than_hours":-5}`} {
		if rec := e.do(true, http.MethodPost, "/api/nodes/cleanup", body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %q = %d, want 400", body, rec.Code)
		}
	}
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&n)
	if n != 1 {
		t.Fatal("a rejected cleanup deleted nodes")
	}
	if rec := e.do(true, http.MethodPost, "/api/nodes/cleanup", `{"all":true}`); rec.Code != http.StatusOK {
		t.Fatalf("explicit all = %d %s", rec.Code, rec.Body)
	}
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&n)
	if n != 0 || e.auditCount("node.cleanup") != 1 {
		t.Fatalf("nodes left %d, node.cleanup audit rows %d", n, e.auditCount("node.cleanup"))
	}
}

func TestBackupPlanRetentionAndSchedule(t *testing.T) {
	e := newRouterEnv(t)
	if _, err := e.db.Exec(`INSERT INTO s3_endpoints (id, name, endpoint, access_key, secret_key, created_at, updated_at)
		VALUES ('s3', 's3', 'https://s3.test', 'a', 'b', 0, 0)`); err != nil {
		t.Fatal(err)
	}
	plan := func(extra string) string {
		return `{"name":"p","sources":["/data"],"s3_endpoint_id":"s3","s3_bucket":"b"` + extra + `}`
	}
	keepOf := func(name string) int {
		var k int
		if err := e.db.QueryRow(`SELECT retention_keep FROM backup_plans WHERE name = ?`, name).Scan(&k); err != nil {
			t.Fatal(err)
		}
		return k
	}

	rec := e.do(true, http.MethodPost, "/api/backups/plans", plan(`,"schedule":"0 3 * * *"`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "6 fields") {
		t.Fatalf("5-field schedule = %d %s, want 400 explaining the format", rec.Code, rec.Body)
	}
	if rec := e.do(true, http.MethodPost, "/api/backups/plans",
		strings.Replace(plan(`,"schedule":"0 0 3 * * *","retention_keep":0`), `"p"`, `"zero"`, 1)); rec.Code != http.StatusOK {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	if k := keepOf("zero"); k != 0 {
		t.Fatalf("retention_keep 0 (keep everything) was stored as %d", k)
	}
	if rec := e.do(true, http.MethodPost, "/api/backups/plans",
		strings.Replace(plan(`,"schedule":"@daily"`), `"p"`, `"dflt"`, 1)); rec.Code != http.StatusOK {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	if k := keepOf("dflt"); k != 7 {
		t.Fatalf("absent retention_keep stored as %d, want the default 7", k)
	}
	var id string
	_ = e.db.QueryRow(`SELECT id FROM backup_plans WHERE name = 'zero'`).Scan(&id)
	upd := plan(`,"schedule":"every day","retention_keep":3,"key_template":"backups/{host}/x"`)
	if rec := e.do(true, http.MethodPut, "/api/backups/plans/"+id, upd); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid schedule on update = %d, want 400", rec.Code)
	}
}

func TestSecurityHeadersAndReadyz(t *testing.T) {
	e := newRouterEnv(t)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "agents") {
		t.Fatalf("readyz = %d %s; must not report agent counts", rec.Code, rec.Body)
	}
	for k, want := range map[string]string{
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": "frame-ancestors 'none'",
	} {
		if got := rec.Header().Get(k); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS on a plain-HTTP request without SESSION_SECURE_COOKIE")
	}

	// A spoofed X-Forwarded-Proto from an untrusted peer must not turn HSTS on.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("untrusted X-Forwarded-Proto enabled HSTS")
	}
}

func TestBodyLimit(t *testing.T) {
	e := newRouterEnv(t)
	big := `{"name":"` + strings.Repeat("a", maxBodyBytes+1) + `"}`
	if rec := e.do(true, http.MethodPost, "/api/webhooks", big); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body = %d, want 400", rec.Code)
	}
}

func TestSSEHidesBackupTopicsFromNonAdmins(t *testing.T) {
	user := &subscriber{userID: 1}
	admin := &subscriber{userID: 2, isAdmin: true}
	anon := &subscriber{}
	for _, topic := range []string{"backup.started", "backup.node.finished"} {
		ev := Event{Topic: topic}
		if user.visible(ev) || !admin.visible(ev) {
			t.Errorf("%s: user=%t admin=%t", topic, user.visible(ev), admin.visible(ev))
		}
	}
	if !user.visible(Event{Topic: "node.metrics"}) || anon.visible(Event{Topic: "node.metrics"}) {
		t.Error("node topics go to every signed-in user and to nobody else")
	}
}

// The server's ReadTimeout must not end SSE streams (a WriteTimeout would).
func TestSSESurvivesReadTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := NewHub()
	go hub.Run(ctx)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.serve(w, r, 1, false)
	}))
	srv.Config.ReadTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	time.Sleep(300 * time.Millisecond)
	hub.Publish("node.metrics", map[string]any{"ok": true})

	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var all string
		for {
			n, err := resp.Body.Read(buf)
			all += string(buf[:n])
			if strings.Contains(all, "event: node.metrics") || err != nil {
				got <- all
				return
			}
		}
	}()
	select {
	case s := <-got:
		if !strings.Contains(s, "event: node.metrics") {
			t.Fatalf("stream ended before the event arrived: %q", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3s")
	}
}
