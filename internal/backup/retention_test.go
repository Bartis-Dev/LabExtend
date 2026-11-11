package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/notify"
	"github.com/Bartis-Dev/LabExtend/internal/s3"
)

// fakeDeleter records every DeleteKeys call and fails the keys in fail with
// the given error code.
type fakeDeleter struct {
	calls [][]string
	fail  map[string]string // key -> code
	err   error             // whole-call error
}

func (f *fakeDeleter) DeleteKeys(_ context.Context, _ string, keys []string) ([]s3.DeleteError, error) {
	f.calls = append(f.calls, append([]string(nil), keys...))
	if f.err != nil {
		return nil, f.err
	}
	var out []s3.DeleteError
	for _, k := range keys {
		if code, ok := f.fail[k]; ok {
			out = append(out, s3.DeleteError{Key: k, Code: code, Message: code})
		}
	}
	return out, nil
}

func (f *fakeDeleter) deleted() map[string]bool {
	m := map[string]bool{}
	for _, c := range f.calls {
		for _, k := range c {
			if _, failed := f.fail[k]; !failed && f.err == nil {
				m[k] = true
			}
		}
	}
	return m
}

func retentionDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newTestDB(t)
	// :memory: is per connection; one connection keeps every query on the same DB.
	db.SetMaxOpenConns(1)
	return db
}

func seedRetentionPlan(t *testing.T, db *sql.DB, id string, keep int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO backup_plans (id, name, sources_json, scope_type, s3_endpoint_id,
		s3_bucket, key_template, retention_keep, compression, compression_level, webhook_mode, engine, verify_restore)
		VALUES (?, ?, '[]', 'all', 's3', 'bucket', 'b/{host}/{date}.tar.gz', ?, 'zstd', 3, 'off', 'tar', 0)`,
		id, id, keep); err != nil {
		t.Fatal(err)
	}
}

// seedRun inserts a run and one item per node; status per node comes from
// statuses (default success). Keys are "<plan>/<node>/<runID>".
func seedRun(t *testing.T, db *sql.DB, planID, runID string, startedAt int64, runStatus string, nodes []string, failedNodes ...string) {
	t.Helper()
	failed := map[string]bool{}
	for _, n := range failedNodes {
		failed[n] = true
	}
	var results []NodeResult
	for _, n := range nodes {
		st := "success"
		if failed[n] {
			st = "failed"
		}
		results = append(results, NodeResult{NodeID: n, Status: st, S3Key: planID + "/" + n + "/" + runID})
	}
	excerpt, _ := json.Marshal(results)
	if _, err := db.Exec(`INSERT INTO backup_runs (id, plan_id, triggered_by, started_at, finished_at, status, error_summary, log_excerpt)
		VALUES (?, ?, 'schedule', ?, ?, ?, '', ?)`, runID, planID, startedAt, startedAt+60, runStatus, string(excerpt)); err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if _, err := db.Exec(`INSERT INTO backup_run_items (run_id, node_id, started_at, finished_at, status, bytes_uploaded, s3_key)
			VALUES (?, ?, ?, ?, ?, 100, ?)`, runID, res.NodeID, startedAt, startedAt+60, res.Status, res.S3Key); err != nil {
			t.Fatal(err)
		}
	}
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func runExists(t *testing.T, db *sql.DB, id string) bool {
	return count(t, db, `SELECT COUNT(*) FROM backup_runs WHERE id = ?`, id) == 1
}

func loadTestPlan(t *testing.T, db *sql.DB, id string) plan {
	t.Helper()
	p, err := (&Runner{db: db}).loadPlan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPruneKeepsNewestPerNodeAndLeavesOtherPlansAlone(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 2)
	seedRetentionPlan(t, db, "p2", 2)
	nodes := []string{"n1", "n2"}
	for i := 1; i <= 4; i++ {
		seedRun(t, db, "p1", fmt.Sprintf("r%d", i), int64(i*1000), "success", nodes)
		seedRun(t, db, "p2", fmt.Sprintf("q%d", i), int64(i*1000), "success", nodes)
	}
	// n3 only joined for the last run: its single item is within keep.
	seedRun(t, db, "p1", "r5", 5000, "success", []string{"n1", "n2", "n3"})

	del := &fakeDeleter{}
	res, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r5")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	want := map[string]bool{"p1/n1/r1": true, "p1/n2/r1": true, "p1/n1/r2": true, "p1/n2/r2": true, "p1/n1/r3": true, "p1/n2/r3": true}
	got := del.deleted()
	if len(got) != len(want) {
		t.Fatalf("deleted %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("%s not deleted", k)
		}
	}
	if res.Objects != 6 || res.Bytes != 600 {
		t.Errorf("result = %+v, want 6 objects / 600 bytes", res)
	}
	for _, id := range []string{"r1", "r2", "r3"} {
		if runExists(t, db, id) {
			t.Errorf("run %s should be gone once empty", id)
		}
	}
	if !runExists(t, db, "r4") || !runExists(t, db, "r5") {
		t.Error("kept runs were removed")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM backup_run_items i JOIN backup_runs r ON r.id = i.run_id WHERE r.plan_id = 'p2'`); n != 8 {
		t.Errorf("plan p2 items = %d, want 8 untouched", n)
	}
}

func TestPruneNeverTouchesCurrentRun(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "old", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "new", 2000, "success", []string{"n1"})
	del := &fakeDeleter{}
	// Contrived: the "current" run ranks second, so keep=1 would otherwise prune it.
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "old"); err != nil {
		t.Fatal(err)
	}
	if len(del.calls) != 0 || !runExists(t, db, "old") {
		t.Errorf("current run was pruned: calls=%v", del.calls)
	}
}

func TestPruneKeepsRowsForKeysThatFailedToDelete(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1", "n2"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1", "n2"})
	del := &fakeDeleter{fail: map[string]string{"p1/n1/r1": "AccessDenied"}}
	res, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r2")
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("err = %v, want AccessDenied surfaced", err)
	}
	if res.Objects != 1 || res.Failed != 1 {
		t.Errorf("result = %+v, want 1 deleted, 1 failed", res)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM backup_run_items WHERE s3_key = 'p1/n1/r1'`); n != 1 {
		t.Error("row for a key R2 did not confirm was deleted")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM backup_run_items WHERE s3_key = 'p1/n2/r1'`); n != 0 {
		t.Error("row for a confirmed key was kept")
	}
	if !runExists(t, db, "r1") {
		t.Fatal("run with a remaining item was removed")
	}
	// The UI reads per-node detail from log_excerpt; the pruned node must leave it.
	var excerpt string
	_ = db.QueryRow(`SELECT log_excerpt FROM backup_runs WHERE id='r1'`).Scan(&excerpt)
	if strings.Contains(excerpt, "p1/n2/r1") || !strings.Contains(excerpt, "p1/n1/r1") {
		t.Errorf("excerpt not filtered to remaining items: %s", excerpt)
	}
}

func TestPruneWholeCallErrorKeepsRows(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1"})
	del := &fakeDeleter{err: errors.New("connection reset")}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r2"); err == nil {
		t.Fatal("expected error")
	}
	if !runExists(t, db, "r1") || count(t, db, `SELECT COUNT(*) FROM backup_run_items WHERE run_id='r1'`) != 1 {
		t.Error("rows removed although nothing was confirmed deleted")
	}
}

func TestPruneTreatsNoSuchKeyAsDeleted(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1"})
	del := &fakeDeleter{fail: map[string]string{"p1/n1/r1": "NoSuchKey"}}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r2"); err != nil {
		t.Fatalf("NoSuchKey must not be an error: %v", err)
	}
	if runExists(t, db, "r1") {
		t.Error("run whose object is already gone was kept")
	}
}

func TestPruneKeepZeroDeletesNothing(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 0)
	for i := 1; i <= 5; i++ {
		seedRun(t, db, "p1", fmt.Sprintf("r%d", i), int64(i*1000), "success", []string{"n1"})
	}
	del := &fakeDeleter{}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r5"); err != nil {
		t.Fatal(err)
	}
	if len(del.calls) != 0 || count(t, db, `SELECT COUNT(*) FROM backup_runs`) != 5 {
		t.Errorf("keep=0 deleted something: calls=%v", del.calls)
	}
}

// Two runs on the same day render the same key under a {date} template; the
// older row goes, but the object belongs to the kept one and must stay.
func TestPruneDoesNotDeleteObjectStillReferenced(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1"})
	if _, err := db.Exec(`UPDATE backup_run_items SET s3_key = 'same' `); err != nil {
		t.Fatal(err)
	}
	del := &fakeDeleter{}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r2"); err != nil {
		t.Fatal(err)
	}
	if len(del.deleted()) != 0 {
		t.Errorf("deleted an object a kept item still references: %v", del.calls)
	}
	if runExists(t, db, "r1") {
		t.Error("superseded run row should be removed")
	}
}

func TestPruneBatchesAt1000Keys(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 2)
	tx, _ := db.Begin()
	for i := 0; i < 1003; i++ {
		id := fmt.Sprintf("r%04d", i)
		tx.Exec(`INSERT INTO backup_runs (id, plan_id, triggered_by, started_at, status) VALUES (?, 'p1', 's', ?, 'success')`, id, i)
		tx.Exec(`INSERT INTO backup_run_items (run_id, node_id, started_at, status, bytes_uploaded, s3_key) VALUES (?, 'n1', ?, 'success', 1, ?)`, id, i, "k"+id)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	del := &fakeDeleter{}
	res, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r1002")
	if err != nil {
		t.Fatal(err)
	}
	if len(del.calls) != 2 || len(del.calls[0]) != 1000 || len(del.calls[1]) != 1 || res.Objects != 1001 {
		t.Errorf("calls=%d res=%+v", len(del.calls), res)
	}
}

func TestCleanupExpiredRemovesOldFailures(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 0)
	now := time.Unix(100*86400, 0)
	old := now.Add(-31 * 24 * time.Hour).Unix()
	young := now.Add(-29 * 24 * time.Hour).Unix()

	// Early failure: a run with no items at all.
	if _, err := db.Exec(`INSERT INTO backup_runs (id, plan_id, triggered_by, started_at, status) VALUES
		('early-old', 'p1', 's', ?, 'failed'), ('early-young', 'p1', 's', ?, 'failed'), ('stuck', 'p1', 's', ?, 'running')`,
		old, young, old); err != nil {
		t.Fatal(err)
	}
	seedRun(t, db, "p1", "failed-old", old, "failed", []string{"n1"}, "n1")
	seedRun(t, db, "p1", "partial-old", old, "partial", []string{"n1", "n2"}, "n2")
	seedRun(t, db, "p1", "failed-young", young, "failed", []string{"n1"}, "n1")
	seedRun(t, db, "p1", "success-old", old, "success", []string{"n1"})

	if err := cleanupExpired(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{
		"early-old": false, "failed-old": false, "early-young": true, "failed-young": true,
		"stuck": true, "success-old": true, "partial-old": true,
	} {
		if got := runExists(t, db, id); got != want {
			t.Errorf("run %s exists=%v, want %v", id, got, want)
		}
	}
	// The partial run keeps its successful item; only the failed one ages out.
	if n := count(t, db, `SELECT COUNT(*) FROM backup_run_items WHERE run_id='partial-old'`); n != 1 {
		t.Errorf("partial-old items = %d, want 1", n)
	}
	var excerpt string
	_ = db.QueryRow(`SELECT log_excerpt FROM backup_runs WHERE id='partial-old'`).Scan(&excerpt)
	if strings.Contains(excerpt, `"n2"`) {
		t.Errorf("expired failed node still in excerpt: %s", excerpt)
	}
}

// A prune failure must be visible on the run without failing the backup.
func TestRetentionFailureIsNotedOnSuccessfulRun(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1"})
	r := &Runner{db: db, events: notify.New(db, nil)}
	r.retainWith(context.Background(), loadTestPlan(t, db, "p1"), "r2", &fakeDeleter{fail: map[string]string{"p1/n1/r1": "AccessDenied"}})

	var status, summary string
	_ = db.QueryRow(`SELECT status, error_summary FROM backup_runs WHERE id='r2'`).Scan(&status, &summary)
	if status != "success" || !strings.Contains(summary, "retention") {
		t.Errorf("status=%q summary=%q", status, summary)
	}
	n := onlyNotification(t, db)
	if n.typ != notify.BackupRetentionFailed || n.link != "/backups?run=r2" || !strings.Contains(n.body, "AccessDenied") {
		t.Errorf("notification = %+v", n)
	}
}

func TestRetentionReportDeletesNothing(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1"})
	db.Exec(`INSERT INTO nodes (id, hostname) VALUES ('n1', 'host-one')`)

	var buf bytes.Buffer
	if err := RetentionReport(context.Background(), &buf, db, "", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "host-one") || !strings.Contains(out, "p1") {
		t.Errorf("report missing plan/node:\n%s", out)
	}
	if count(t, db, `SELECT COUNT(*) FROM backup_run_items`) != 2 {
		t.Error("dry run deleted rows")
	}
}

// A multi-DSN pgdump item owns several objects; all of them must go.
func TestPruneDeletesEveryObjectOfAnItem(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1"})
	db.Exec(`UPDATE backup_run_items SET extra_s3_keys = '["' || s3_key || '.1"]'`)
	del := &fakeDeleter{}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r2"); err != nil {
		t.Fatal(err)
	}
	got := del.deleted()
	if len(got) != 2 || !got["p1/n1/r1"] || !got["p1/n1/r1.1"] {
		t.Errorf("deleted %v, want both objects of r1", got)
	}
	if runExists(t, db, "r1") {
		t.Error("row kept although all its objects were deleted")
	}
}

// If one object of an item fails to delete, the row must stay so the next
// prune retries it.
func TestPruneKeepsRowWhenOneOfItsObjectsFails(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1"})
	db.Exec(`UPDATE backup_run_items SET extra_s3_keys = '["' || s3_key || '.1"]'`)
	del := &fakeDeleter{fail: map[string]string{"p1/n1/r1.1": "AccessDenied"}}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r2"); err == nil {
		t.Fatal("expected error")
	}
	if !runExists(t, db, "r1") {
		t.Error("row removed although one of its objects is still in the bucket")
	}
}

// started_at comes from the leader clock; the autoincrement id does not jump
// back when that clock does.
func TestPruneRanksByIDNotClock(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 5000, "success", []string{"n1"}) // clock was ahead
	seedRun(t, db, "p1", "r2", 1000, "success", []string{"n1"})
	del := &fakeDeleter{}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r2"); err != nil {
		t.Fatal(err)
	}
	if got := del.deleted(); len(got) != 1 || !got["p1/n1/r1"] {
		t.Errorf("deleted %v, want the older run r1", got)
	}
}

// A key referenced by a plan in another bucket names a different object.
func TestPruneReferenceProtectionIsPerBucket(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRetentionPlan(t, db, "other", 0)
	seedRetentionPlan(t, db, "same", 0)
	db.Exec(`UPDATE backup_plans SET s3_bucket = 'elsewhere' WHERE id = 'other'`)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	seedRun(t, db, "p1", "r2", 2000, "success", []string{"n1"})
	seedRun(t, db, "other", "o1", 1000, "success", []string{"n1"})
	db.Exec(`UPDATE backup_run_items SET s3_key = 'p1/n1/r1' WHERE run_id = 'o1'`)
	del := &fakeDeleter{}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r2"); err != nil {
		t.Fatal(err)
	}
	if !del.deleted()["p1/n1/r1"] {
		t.Error("object kept because a plan in another bucket uses the same key")
	}

	// Same bucket: the reference is real and protects the object.
	seedRun(t, db, "p1", "r3", 3000, "success", []string{"n1"})
	seedRun(t, db, "same", "s1", 1000, "success", []string{"n1"})
	db.Exec(`UPDATE backup_run_items SET s3_key = 'p1/n1/r2' WHERE run_id = 's1'`)
	del = &fakeDeleter{}
	if _, err := pruneRetention(context.Background(), db, del, loadTestPlan(t, db, "p1"), "r3"); err != nil {
		t.Fatal(err)
	}
	if del.deleted()["p1/n1/r2"] {
		t.Error("deleted an object another plan in the same bucket references")
	}
}

// The prune's own context is what times out; recording that must not use it.
func TestRetentionErrorIsNotedOnExpiredContext(t *testing.T) {
	db := retentionDB(t)
	seedRetentionPlan(t, db, "p1", 1)
	seedRun(t, db, "p1", "r1", 1000, "success", []string{"n1"})
	r := &Runner{db: db, events: notify.New(db, nil)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.noteRetentionError(ctx, loadTestPlan(t, db, "p1"), "r1", context.DeadlineExceeded)
	var summary string
	_ = db.QueryRow(`SELECT error_summary FROM backup_runs WHERE id='r1'`).Scan(&summary)
	if !strings.Contains(summary, "retention") {
		t.Errorf("summary = %q", summary)
	}
	if count(t, db, `SELECT COUNT(*) FROM notifications`) != 1 {
		t.Error("no retention notification on an expired context")
	}
}
