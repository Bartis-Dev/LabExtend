package backup

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"

	_ "modernc.org/sqlite"
)

// A backup that dies before the fan-out used to be recorded as failed and
// announced nowhere, because the webhook only fired at the bottom of run().
// Six nightly runs were lost that way. These tests pin the two halves of that
// contract: an early failure must reach the webhook, and a success under
// on-error must still stay quiet.

type fakeAgents struct{ list []AgentInfo }

func (f fakeAgents) ListAgents() []AgentInfo { return f.list }
func (f fakeAgents) Request(context.Context, string, *pb.Command) (*pb.CommandResult, error) {
	return nil, nil
}

// capture records every webhook body the runner posts.
type capture struct {
	mu     sync.Mutex
	bodies []string
}

func (c *capture) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, s)
}

func (c *capture) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.bodies...)
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Only the columns the runner actually reads or writes.
	schema := `
		CREATE TABLE backup_plans (
			id TEXT PRIMARY KEY, name TEXT, sources_json TEXT, scope_type TEXT,
			scope_value TEXT, s3_endpoint_id TEXT, s3_bucket TEXT, key_template TEXT,
			retention_keep INT, compression TEXT, compression_level INT,
			webhook_id TEXT, webhook_mode TEXT, engine TEXT, verify_restore INT);
		CREATE TABLE backup_runs (
			id TEXT PRIMARY KEY, plan_id TEXT, triggered_by TEXT, started_at INT,
			finished_at INT, status TEXT, error_summary TEXT, log_excerpt TEXT);
		CREATE TABLE backup_run_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT, node_id TEXT,
			started_at INT, finished_at INT, status TEXT, error TEXT, bytes_uploaded INT, s3_key TEXT);
		CREATE TABLE s3_endpoints (
			id TEXT PRIMARY KEY, endpoint TEXT, region TEXT, access_key TEXT,
			secret_key TEXT, path_style INT);
		CREATE TABLE webhook_configs (
			id TEXT PRIMARY KEY, name TEXT, kind TEXT, url TEXT, enabled INT,
			created_at INT, updated_at INT);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// seedPlan wires a plan to a webhook and an S3 endpoint. secretKey is stored
// as-is: a value that is not valid AES-GCM ciphertext makes loadS3Endpoint
// fail exactly the way the August 2026 incident did.
func seedPlan(t *testing.T, db *sql.DB, webhookURL, mode, secretKey string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO webhook_configs (id, name, kind, url, enabled) VALUES ('wh', 'test', 'discord', ?, 1)`,
		webhookURL); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO s3_endpoints (id, endpoint, region, access_key, secret_key, path_style)
		 VALUES ('s3', 'https://example.invalid', 'auto', 'ak', ?, 0)`, secretKey); err != nil {
		t.Fatalf("seed s3: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO backup_plans (id, name, sources_json, scope_type, scope_value,
		   s3_endpoint_id, s3_bucket, key_template, retention_keep, compression,
		   compression_level, webhook_id, webhook_mode, engine, verify_restore)
		 VALUES ('p1', 'Nightly', '["/data"]', 'all', '', 's3', 'bucket', '{date}', 0, 'zstd', 3, 'wh', ?, 'tar', 0)`,
		mode); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
}

func startCapture(t *testing.T) (*capture, string) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		cap.add(string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return cap, srv.URL
}

// The regression: the run fails while decrypting the S3 secret, long before
// any node is contacted, and must still announce itself.
func TestEarlyS3FailureFiresWebhook(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "not-valid-ciphertext")

	r := NewRunner(db, fakeAgents{list: []AgentInfo{{ID: "n1", Hostname: "node-1"}}}, nil,
		"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	r.run("p1", "Nightly", "run-1", "schedule")

	bodies := cap.all()
	if len(bodies) != 1 {
		t.Fatalf("expected exactly 1 webhook post, got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], "failed") {
		t.Errorf("payload does not report failure: %s", bodies[0])
	}
	// Without the reason the message is unactionable, which was half the problem.
	if !strings.Contains(bodies[0], "s3 endpoint") {
		t.Errorf("payload omits the cause: %s", bodies[0])
	}

	var status, summary string
	if err := db.QueryRow(`SELECT status, error_summary FROM backup_runs WHERE id='run-1'`).
		Scan(&status, &summary); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if status != "failed" {
		t.Errorf("run status = %q, want failed", status)
	}
}

// An empty scope is the other pre-fan-out exit.
func TestEmptyScopeFiresWebhook(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "not-valid-ciphertext")

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef")
	r.run("p1", "Nightly", "run-2", "schedule")

	bodies := cap.all()
	if len(bodies) != 1 {
		t.Fatalf("expected exactly 1 webhook post, got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], "no agents matched") {
		t.Errorf("payload omits the cause: %s", bodies[0])
	}
}

// on-error must stay quiet when there is no error - otherwise the fix would
// turn a silent channel into a noisy one, and noise gets muted.
func TestOnErrorStaysQuietOnSuccess(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "not-valid-ciphertext")

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef")
	p, err := r.loadPlan(context.Background(), "p1")
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	r.notify(context.Background(), p, "success", "", "Nightly", "run-3", 123, time.Second, nil)

	if got := len(cap.all()); got != 0 {
		t.Fatalf("expected no webhook post on success, got %d", got)
	}
}

// A plan with no webhook configured must not panic or post.
func TestNoWebhookConfigured(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "not-valid-ciphertext")
	if _, err := db.Exec(`UPDATE backup_plans SET webhook_id = NULL WHERE id='p1'`); err != nil {
		t.Fatalf("clear webhook: %v", err)
	}

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef")
	r.run("p1", "Nightly", "run-4", "schedule")

	if got := len(cap.all()); got != 0 {
		t.Fatalf("expected no webhook post, got %d", got)
	}
}
