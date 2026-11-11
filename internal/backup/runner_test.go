package backup

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
	"github.com/Bartis-Dev/LabExtend/internal/notify"

	_ "modernc.org/sqlite"
)

// A backup that dies before the fan-out used to be recorded as failed and
// announced nowhere, because the webhook only fired at the bottom of run().
// Six nightly runs were lost that way. These tests pin the two halves of that
// contract: an early failure must reach the webhook (now as a backup.failed
// event), and a success under on-error must still stay quiet.

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
			started_at INT, finished_at INT, status TEXT, error TEXT, bytes_uploaded INT, s3_key TEXT,
			file_count INT, sha256 TEXT, extra_s3_keys TEXT NOT NULL DEFAULT '[]');
		CREATE TABLE nodes (id TEXT PRIMARY KEY, hostname TEXT, labels_json TEXT NOT NULL DEFAULT '{}');
		CREATE TABLE s3_endpoints (
			id TEXT PRIMARY KEY, endpoint TEXT, region TEXT, access_key TEXT,
			secret_key TEXT, path_style INT);
		CREATE TABLE webhook_configs (
			id TEXT PRIMARY KEY, name TEXT, kind TEXT, url TEXT, enabled INT,
			created_at INT, updated_at INT, event_types TEXT NOT NULL DEFAULT '[]');
		CREATE TABLE notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, ts INT, type TEXT, severity TEXT,
			title TEXT, body TEXT, link TEXT, details_json TEXT, read_at INT,
			delivery_error TEXT NOT NULL DEFAULT '');
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
		`INSERT INTO webhook_configs (id, name, kind, url, enabled, event_types) VALUES ('wh', 'test', 'discord', ?, 1, '["backup.failed"]')`,
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
		"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff", notify.New(db, nil))
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
	n := onlyNotification(t, db)
	if n.typ != notify.BackupFailed || n.severity != notify.SeverityCritical || n.link != "/backups?run=run-1" {
		t.Errorf("notification = %+v", n)
	}
}

type storedNotification struct{ typ, severity, title, body, link, details string }

func onlyNotification(t *testing.T, db *sql.DB) storedNotification {
	t.Helper()
	rows, err := db.Query(`SELECT type, severity, title, body, link, details_json FROM notifications`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedNotification
	for rows.Next() {
		var n storedNotification
		if err := rows.Scan(&n.typ, &n.severity, &n.title, &n.body, &n.link, &n.details); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if len(out) != 1 {
		t.Fatalf("want exactly 1 notification, got %+v", out)
	}
	return out[0]
}

// An empty scope is the other pre-fan-out exit.
func TestEmptyScopeFiresWebhook(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "not-valid-ciphertext")

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef", notify.New(db, nil))
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

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef", notify.New(db, nil))
	p, err := r.loadPlan(context.Background(), "p1")
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	r.notify(context.Background(), p, "success", "", "Nightly", "run-3", 123, time.Second, nil)

	if got := len(cap.all()); got != 0 {
		t.Fatalf("expected no webhook post on success, got %d", got)
	}
}

// Failures no longer depend on the plan's own webhook: a plan without one still
// reaches every webhook subscribed to backup.failed.
func TestFailureWithoutPlanWebhookReachesSubscribers(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "off", "not-valid-ciphertext")
	if _, err := db.Exec(`UPDATE backup_plans SET webhook_id = NULL WHERE id='p1'`); err != nil {
		t.Fatalf("clear webhook: %v", err)
	}

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef", notify.New(db, nil))
	r.run("p1", "Nightly", "run-4", "schedule")

	if got := len(cap.all()); got != 1 {
		t.Fatalf("expected 1 webhook post, got %d", got)
	}
}

// A webhook that did not subscribe to backup.failed hears nothing, but the
// failure is still kept for the panel.
func TestFailureSkipsUnsubscribedWebhook(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "not-valid-ciphertext")
	if _, err := db.Exec(`UPDATE webhook_configs SET event_types = '["node.offline"]'`); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef", notify.New(db, nil))
	r.run("p1", "Nightly", "run-5", "schedule")

	if got := len(cap.all()); got != 0 {
		t.Fatalf("unsubscribed webhook got %d posts", got)
	}
	onlyNotification(t, db)
}

// A partial run names the nodes that failed and why.
func TestPartialRunEmitsFailedNodes(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "x")

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef", notify.New(db, nil))
	p, err := r.loadPlan(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	r.notify(context.Background(), p, "partial", "", "Nightly", "run-6", 10, time.Second, []NodeResult{
		{NodeID: "n1", Status: "success"},
		{NodeID: "n2", Status: "failed", Error: "AccessDenied: no write"},
	})

	n := onlyNotification(t, db)
	if n.typ != notify.BackupFailed || n.severity != notify.SeverityWarning {
		t.Errorf("notification = %+v", n)
	}
	if !strings.Contains(n.body, "n2: AccessDenied: no write") || strings.Contains(n.body, "n1") {
		t.Errorf("body = %q", n.body)
	}
	if !strings.Contains(n.details, `"failed_nodes":["n2"]`) {
		t.Errorf("details = %s", n.details)
	}
	if got := cap.all(); len(got) != 1 || !strings.Contains(got[0], "partially failed") {
		t.Errorf("posts = %q", got)
	}
}

// webhook_mode=always still announces successful runs on the plan's webhook,
// as a summary rather than a notification.
func TestAlwaysPostsSuccessToPlanWebhook(t *testing.T) {
	db := newTestDB(t)
	cap, url := startCapture(t)
	seedPlan(t, db, url, "always", "x")

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef", notify.New(db, nil))
	p, err := r.loadPlan(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	r.notify(context.Background(), p, "success", "", "Nightly", "run-7", 10, time.Second, nil)

	if got := cap.all(); len(got) != 1 || !strings.Contains(got[0], "Backup success") {
		t.Fatalf("posts = %q", got)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&n)
	if n != 0 {
		t.Fatalf("a successful run created %d notifications", n)
	}
}

// A run cut off by a leader restart is a failed backup like any other.
func TestReconcileStaleRunsEmitsFailure(t *testing.T) {
	db := newTestDB(t)
	_, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "x")
	if _, err := db.Exec(`INSERT INTO backup_runs (id, plan_id, triggered_by, started_at, status)
		VALUES ('run-8', 'p1', 'schedule', 1, 'running')`); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(db, fakeAgents{list: nil}, nil, "deadbeef", notify.New(db, nil))
	r.ReconcileStaleRuns(context.Background())

	n := onlyNotification(t, db)
	if n.typ != notify.BackupFailed || !strings.Contains(n.title, "Nightly") || !strings.Contains(n.body, "leader restarted") {
		t.Errorf("notification = %+v", n)
	}
}

// replyAgents answers every RunBackup with a fixed response.
type replyAgents struct {
	list  []AgentInfo
	reply *pb.RunBackupResp
}

func (f replyAgents) ListAgents() []AgentInfo { return f.list }
func (f replyAgents) Request(context.Context, string, *pb.Command) (*pb.CommandResult, error) {
	return &pb.CommandResult{Ok: true, Payload: &pb.CommandResult_RunBackup{RunBackup: f.reply}}, nil
}

const testSecretsKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// encryptForTest mirrors the handlers_s3 format decryptAESGCM reads.
func encryptForTest(t *testing.T, plain string) string {
	t.Helper()
	key, _ := hex.DecodeString(testSecretsKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
}

// A pgdump plan with several DSNs uploads one object per source and none at
// the rendered key. Recording only the rendered key made retention delete a
// key that never existed and drop the row, so the real dumps leaked forever.
func TestRunRecordsEveryUploadedKey(t *testing.T) {
	db := newTestDB(t)
	_, url := startCapture(t)
	seedPlan(t, db, url, "on-error", encryptForTest(t, "sk"))
	db.Exec(`INSERT INTO nodes (id, hostname) VALUES ('n1', 'node-1')`)
	r := NewRunner(db, replyAgents{
		list:  []AgentInfo{{ID: "n1", Hostname: "node-1"}},
		reply: &pb.RunBackupResp{S3Key: "k.0.a.dump", S3Keys: []string{"k.0.a.dump", "k.1.b.dump"}, BytesUploaded: 10},
	}, nil, testSecretsKey, notify.New(db, nil))
	r.run("p1", "Nightly", "run-1", "schedule")

	var key, extra string
	if err := db.QueryRow(`SELECT s3_key, extra_s3_keys FROM backup_run_items WHERE run_id = 'run-1'`).Scan(&key, &extra); err != nil {
		t.Fatal(err)
	}
	if key != "k.0.a.dump" || extra != `["k.1.b.dump"]` {
		t.Errorf("s3_key=%q extra=%q", key, extra)
	}
}

// A known node that is not connected used to be skipped silently, and the run
// reported success without its backup.
func TestRunFailsDisconnectedNodesInScope(t *testing.T) {
	db := newTestDB(t)
	_, url := startCapture(t)
	seedPlan(t, db, url, "on-error", encryptForTest(t, "sk"))
	db.Exec(`INSERT INTO nodes (id, hostname) VALUES ('n1', 'node-1'), ('n2', 'node-2')`)
	r := NewRunner(db, replyAgents{
		list:  []AgentInfo{{ID: "n1", Hostname: "node-1"}},
		reply: &pb.RunBackupResp{S3Key: "k"},
	}, nil, testSecretsKey, notify.New(db, nil))
	r.run("p1", "Nightly", "run-1", "schedule")

	var status string
	_ = db.QueryRow(`SELECT status FROM backup_runs WHERE id = 'run-1'`).Scan(&status)
	if status != "partial" {
		t.Errorf("run status = %q, want partial", status)
	}
	var itemStatus, itemErr string
	if err := db.QueryRow(`SELECT status, error FROM backup_run_items WHERE run_id = 'run-1' AND node_id = 'n2'`).Scan(&itemStatus, &itemErr); err != nil {
		t.Fatalf("no item for the disconnected node: %v", err)
	}
	if itemStatus != "failed" || !strings.Contains(itemErr, "not connected") {
		t.Errorf("item = %q %q", itemStatus, itemErr)
	}
	n := onlyNotification(t, db)
	if n.typ != notify.BackupFailed || !strings.Contains(n.body, "n2") {
		t.Errorf("notification = %+v", n)
	}
}

func TestResolveScopeLabelMissingNodes(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO nodes (id, hostname, labels_json) VALUES
		('n1', 'a', '{"role":"db"}'), ('n2', 'b', '{"role":"db"}'), ('n3', 'c', '{"role":"web"}')`)
	r := NewRunner(db, fakeAgents{list: []AgentInfo{{ID: "n1", Labels: map[string]string{"role": "db"}}}}, nil, "", nil)
	targets, missing := r.resolveScope(context.Background(), plan{ScopeType: "label", ScopeValue: "role=db"})
	if len(targets) != 1 || len(missing) != 1 || missing[0].ID != "n2" {
		t.Errorf("targets=%v missing=%v", targets, missing)
	}
	_, missing = r.resolveScope(context.Background(), plan{ScopeType: "node", ScopeValue: "c"})
	if len(missing) != 1 || missing[0].ID != "n3" {
		t.Errorf("node scope missing=%v", missing)
	}
}

// Only the runs a restarting leader found in flight may be failed: a run
// started after the listing belongs to this leader.
func TestReconcileStaleRunsOnlyTouchesListedRuns(t *testing.T) {
	db := newTestDB(t)
	_, url := startCapture(t)
	seedPlan(t, db, url, "on-error", "x")
	db.Exec(`INSERT INTO backup_runs (id, plan_id, triggered_by, started_at, status) VALUES
		('old', 'p1', 's', 1, 'running'), ('new', 'p1', 's', ?, 'running')`, time.Now().Add(time.Hour).Unix())
	r := NewRunner(db, fakeAgents{}, nil, "", notify.New(db, nil))
	r.ReconcileStaleRuns(context.Background())
	var st string
	_ = db.QueryRow(`SELECT status FROM backup_runs WHERE id = 'new'`).Scan(&st)
	if st != "running" {
		t.Errorf("run started after boot was failed: %q", st)
	}
	_ = db.QueryRow(`SELECT status FROM backup_runs WHERE id = 'old'`).Scan(&st)
	if st != "failed" {
		t.Errorf("stale run status = %q", st)
	}
}
