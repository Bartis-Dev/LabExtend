package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/notify"
	"github.com/Bartis-Dev/LabExtend/internal/s3"
)

// failedRunTTL is how long failed runs and items (which hold no object) stay
// visible before their rows are removed.
const failedRunTTL = 30 * 24 * time.Hour

// deleteBatch is the DeleteObjects per-call key limit.
const deleteBatch = 1000

// keyDeleter is the slice of *s3.Client the prune needs, so tests can fake it.
type keyDeleter interface {
	DeleteKeys(ctx context.Context, bucket string, keys []string) ([]s3.DeleteError, error)
}

// queryer is satisfied by both *sql.DB and *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type retentionItem struct {
	ID     int64
	RunID  string
	NodeID string
	Key    string
	Extra  []string // further objects of the item (multi-DSN pgdump)
	Bytes  int64
	Delete bool
}

// keys lists every object the item owns.
func (it retentionItem) keys() []string {
	if it.Key == "" {
		return it.Extra
	}
	return append([]string{it.Key}, it.Extra...)
}

// PruneResult summarizes one retention pass.
type PruneResult struct {
	Objects int   // objects R2 confirmed deleted (or already gone)
	Bytes   int64 // bytes of those objects
	Failed  int   // objects still in the bucket
	Rows    int   // item rows removed
}

// retentionItems returns every successful item of the plan, flagging those
// beyond the newest retention_keep per node. The current run and in-flight
// runs are never flagged.
func retentionItems(ctx context.Context, q queryer, p plan, currentRunID string) ([]retentionItem, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT i.id, i.run_id, i.node_id, COALESCE(i.s3_key, ''), COALESCE(i.extra_s3_keys, '[]'),
		       COALESCE(i.bytes_uploaded, 0), r.status,
		       -- id, not started_at: the id never runs backwards when the clock does.
		       ROW_NUMBER() OVER (PARTITION BY i.node_id ORDER BY i.id DESC) AS rn
		  FROM backup_run_items i JOIN backup_runs r ON r.id = i.run_id
		 WHERE r.plan_id = ? AND i.status = 'success'
		 ORDER BY i.node_id, rn`, p.ID)
	if err != nil {
		return nil, fmt.Errorf("query retention items: %w", err)
	}
	defer rows.Close()
	var out []retentionItem
	for rows.Next() {
		var it retentionItem
		var runStatus, extra string
		var rn int
		if err := rows.Scan(&it.ID, &it.RunID, &it.NodeID, &it.Key, &extra, &it.Bytes, &runStatus, &rn); err != nil {
			return nil, fmt.Errorf("scan retention item: %w", err)
		}
		if err := json.Unmarshal([]byte(extra), &it.Extra); err != nil {
			return nil, fmt.Errorf("item %d extra_s3_keys: %w", it.ID, err)
		}
		it.Delete = p.RetentionKeep > 0 && rn > p.RetentionKeep && it.RunID != currentRunID &&
			runStatus != "pending" && runStatus != "running"
		out = append(out, it)
	}
	return out, rows.Err()
}

// objectsToDelete returns one entry per distinct object among doomed that no
// surviving row in the same bucket references; Key is that object, and the
// item's bytes count on its first object only. With a {date} template two
// runs on the same day share a key, and the newer one's archive is the object
// that survived.
//
// References are scoped by bucket name only, not by endpoint: two endpoint
// rows can point at the same account, so a same-named bucket elsewhere keeps
// its protection (worst case an object stays, never one deleted under a row).
func objectsToDelete(ctx context.Context, q queryer, p plan, doomed []retentionItem) ([]retentionItem, error) {
	doomedIDs := make(map[int64]bool, len(doomed))
	for _, it := range doomed {
		doomedIDs[it.ID] = true
	}
	// ponytail: loads every item key of the bucket, fine for thousands of rows; filter by key if it grows.
	rows, err := q.QueryContext(ctx, `
		SELECT i.id, COALESCE(i.s3_key, ''), COALESCE(i.extra_s3_keys, '[]')
		  FROM backup_run_items i
		  JOIN backup_runs r ON r.id = i.run_id
		  JOIN backup_plans bp ON bp.id = r.plan_id
		 WHERE bp.s3_bucket = ?`, p.S3Bucket)
	if err != nil {
		return nil, fmt.Errorf("query item keys: %w", err)
	}
	defer rows.Close()
	referenced := map[string]bool{}
	for rows.Next() {
		var id int64
		var key, extra string
		if err := rows.Scan(&id, &key, &extra); err != nil {
			return nil, fmt.Errorf("scan item key: %w", err)
		}
		if doomedIDs[id] {
			continue
		}
		referenced[key] = true
		var more []string
		if err := json.Unmarshal([]byte(extra), &more); err != nil {
			return nil, fmt.Errorf("item %d extra_s3_keys: %w", id, err)
		}
		for _, k := range more {
			referenced[k] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []retentionItem
	for _, it := range doomed {
		for i, k := range it.keys() {
			if k == "" || referenced[k] || seen[k] {
				continue
			}
			seen[k] = true
			obj := it
			obj.Key, obj.Extra = k, nil
			if i > 0 {
				obj.Bytes = 0
			}
			out = append(out, obj)
		}
	}
	return out, nil
}

// pruneRetention deletes the objects and rows of the plan's items beyond
// retention_keep per node. Rows are only removed for objects the provider
// confirmed deleted; NoSuchKey counts as deleted.
func pruneRetention(ctx context.Context, db *sql.DB, del keyDeleter, p plan, currentRunID string) (PruneResult, error) {
	var res PruneResult
	if p.RetentionKeep <= 0 {
		return res, nil
	}
	items, err := retentionItems(ctx, db, p, currentRunID)
	if err != nil {
		return res, err
	}
	var doomed []retentionItem
	for _, it := range items {
		if it.Delete {
			doomed = append(doomed, it)
		}
	}
	if len(doomed) == 0 {
		return res, nil
	}
	objs, err := objectsToDelete(ctx, db, p, doomed)
	if err != nil {
		return res, err
	}

	failed := map[string]bool{}
	var errs []string
	for start := 0; start < len(objs); start += deleteBatch {
		batch := objs[start:min(start+deleteBatch, len(objs))]
		keys := make([]string, len(batch))
		for i, it := range batch {
			keys[i] = it.Key
		}
		bad, err := del.DeleteKeys(ctx, p.S3Bucket, keys)
		if err != nil {
			for _, k := range keys {
				failed[k] = true
			}
			errs = append(errs, err.Error())
			continue
		}
		for _, e := range bad {
			if e.Code == "NoSuchKey" {
				continue
			}
			failed[e.Key] = true
			errs = append(errs, e.Key+": "+e.Code+" "+e.Message)
		}
	}
	for _, it := range objs {
		if failed[it.Key] {
			res.Failed++
		} else {
			res.Objects++
			res.Bytes += it.Bytes
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("begin prune tx: %w", err)
	}
	defer tx.Rollback()
	touched := map[string]bool{}
doomedLoop:
	for _, it := range doomed {
		// One object still in the bucket keeps the row, so the next prune retries it.
		for _, k := range it.keys() {
			if failed[k] {
				continue doomedLoop
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM backup_run_items WHERE id = ?`, it.ID); err != nil {
			return res, fmt.Errorf("delete item %d: %w", it.ID, err)
		}
		res.Rows++
		touched[it.RunID] = true
	}
	for runID := range touched {
		if err := settleRun(ctx, tx, runID); err != nil {
			return res, err
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("commit prune: %w", err)
	}

	if len(errs) > 0 {
		if len(errs) > 3 {
			errs = append(errs[:3], fmt.Sprintf("and %d more", len(errs)-3))
		}
		return res, fmt.Errorf("%d of %d objects not deleted: %s", res.Failed, len(objs), strings.Join(errs, "; "))
	}
	return res, nil
}

// settleRun removes a run that has no items left, otherwise drops the removed
// nodes from log_excerpt, which is where the UI reads per-node detail from.
func settleRun(ctx context.Context, q queryer, runID string) error {
	rows, err := q.QueryContext(ctx, `SELECT node_id FROM backup_run_items WHERE run_id = ?`, runID)
	if err != nil {
		return fmt.Errorf("query run items: %w", err)
	}
	left := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return fmt.Errorf("scan run item: %w", err)
		}
		left[n] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(left) == 0 {
		if _, err := q.ExecContext(ctx, `DELETE FROM backup_runs WHERE id = ?`, runID); err != nil {
			return fmt.Errorf("delete run %s: %w", runID, err)
		}
		return nil
	}
	var excerpt string
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(log_excerpt, '') FROM backup_runs WHERE id = ?`, runID).Scan(&excerpt); err != nil {
		return fmt.Errorf("read excerpt %s: %w", runID, err)
	}
	var results []NodeResult
	if excerpt == "" || json.Unmarshal([]byte(excerpt), &results) != nil {
		return nil
	}
	kept := results[:0]
	for _, n := range results {
		if left[n.NodeID] {
			kept = append(kept, n)
		}
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("encode excerpt %s: %w", runID, err)
	}
	if _, err := q.ExecContext(ctx, `UPDATE backup_runs SET log_excerpt = ? WHERE id = ?`, string(b), runID); err != nil {
		return fmt.Errorf("update excerpt %s: %w", runID, err)
	}
	return nil
}

// cleanupExpired removes failed items and failed runs older than
// failedRunTTL. They hold no object, so only rows go.
func cleanupExpired(ctx context.Context, db *sql.DB, now time.Time) error {
	cutoff := now.Add(-failedRunTTL).Unix()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cleanup tx: %w", err)
	}
	defer tx.Rollback()

	const expired = `
		FROM backup_run_items WHERE status <> 'success' AND started_at < ?
		 AND run_id IN (SELECT id FROM backup_runs WHERE status NOT IN ('pending', 'running'))`
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT run_id `+expired, cutoff)
	if err != nil {
		return fmt.Errorf("query expired items: %w", err)
	}
	var runIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan expired run: %w", err)
		}
		runIDs = append(runIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	itemsRes, err := tx.ExecContext(ctx, `DELETE `+expired, cutoff)
	if err != nil {
		return fmt.Errorf("delete expired items: %w", err)
	}
	for _, id := range runIDs {
		if err := settleRun(ctx, tx, id); err != nil {
			return err
		}
	}
	// Early failures never got items, so settleRun does not see them.
	runsRes, err := tx.ExecContext(ctx, `
		DELETE FROM backup_runs
		 WHERE started_at < ? AND status NOT IN ('pending', 'running', 'success')
		   AND NOT EXISTS (SELECT 1 FROM backup_run_items i WHERE i.run_id = backup_runs.id)`, cutoff)
	if err != nil {
		return fmt.Errorf("delete expired runs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cleanup: %w", err)
	}
	items, _ := itemsRes.RowsAffected()
	runs, _ := runsRes.RowsAffected()
	if items > 0 || runs > 0 || len(runIDs) > 0 {
		slog.Info("backup: removed expired failed runs", "items", items, "empty_runs", runs, "touched_runs", len(runIDs))
	}
	return nil
}

// applyRetention prunes the plan against its real bucket after a run.
func (r *Runner) applyRetention(ctx context.Context, p plan, runID string, cfg s3EndpointCfg) {
	// The run context may be close to its 4h deadline; a prune is still due.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer cancel()
	client, err := s3.NewClient(ctx, s3.EndpointConfig{
		Endpoint: cfg.Endpoint, Region: cfg.Region, PathStyle: cfg.PathStyle,
		Creds: s3.Credentials{AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey},
	})
	if err != nil {
		r.noteRetentionError(ctx, p, runID, fmt.Errorf("s3 client: %w", err))
		return
	}
	r.retainWith(ctx, p, runID, client)
}

func (r *Runner) retainWith(ctx context.Context, p plan, runID string, del keyDeleter) {
	res, err := pruneRetention(ctx, r.db, del, p, runID)
	if res.Rows > 0 || res.Failed > 0 {
		slog.Info("backup: retention pruned", "plan", p.ID, "retention_keep", p.RetentionKeep,
			"objects_deleted", res.Objects, "bytes_freed", res.Bytes, "rows_deleted", res.Rows, "objects_failed", res.Failed)
	}
	if err != nil {
		r.noteRetentionError(ctx, p, runID, err)
	}
}

// noteRetentionError puts a prune failure on the run without changing its
// status: the backup itself succeeded.
func (r *Runner) noteRetentionError(_ context.Context, p plan, runID string, err error) {
	// Most prune failures are the prune context running out; recording that
	// on the same context silently dropped both the note and the event.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	slog.Warn("backup: retention prune failed", "plan", p.ID, "run", runID, "err", err)
	if _, uerr := r.db.ExecContext(ctx, `
		UPDATE backup_runs
		   SET error_summary = CASE WHEN COALESCE(error_summary, '') = '' THEN ? ELSE error_summary || '; ' || ? END
		 WHERE id = ?`, "retention: "+truncate(err.Error(), 1000), "retention: "+truncate(err.Error(), 1000), runID); uerr != nil {
		slog.Warn("backup: recording retention error failed", "run", runID, "err", uerr)
	}
	r.events.Emit(ctx, notify.Event{
		Type: notify.BackupRetentionFailed, Severity: notify.SeverityWarning,
		Title: "Backup retention failed · " + p.Name,
		Body:  "The backup succeeded, but deleting old archives failed: " + truncate(err.Error(), 1000),
		Link:  "/backups?run=" + runID,
		Details: map[string]any{
			"plan_name": p.Name, "run_id": runID, "error": err.Error(),
		},
	})
}

// RetentionReport prints, per plan and node, what the next prune would delete
// and keep, without deleting anything. With listBucket it also lists each
// plan's key prefix and reports objects no row references (report only).
func RetentionReport(ctx context.Context, w io.Writer, db *sql.DB, secretsKey string, listBucket bool) error {
	r := &Runner{db: db, secretsKey: secretsKey}
	hosts := map[string]string{}
	if rows, err := db.QueryContext(ctx, `SELECT id, hostname FROM nodes`); err == nil {
		for rows.Next() {
			var id, h string
			if rows.Scan(&id, &h) == nil {
				hosts[id] = h
			}
		}
		rows.Close()
	}
	var planIDs []string
	rows, err := db.QueryContext(ctx, `SELECT id FROM backup_plans ORDER BY name`)
	if err != nil {
		return fmt.Errorf("list plans: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan plan: %w", err)
		}
		planIDs = append(planIDs, id)
	}
	rows.Close()

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, id := range planIDs {
		p, err := r.loadPlan(ctx, id)
		if err != nil {
			return fmt.Errorf("load plan %s: %w", id, err)
		}
		items, err := retentionItems(ctx, db, p, "")
		if err != nil {
			return err
		}
		var doomed []retentionItem
		for _, it := range items {
			if it.Delete {
				doomed = append(doomed, it)
			}
		}
		objs, err := objectsToDelete(ctx, db, p, doomed)
		if err != nil {
			return err
		}
		type agg struct{ keep, keepB, del, delObj, delB int64 }
		per := map[string]*agg{}
		var nodes []string
		get := func(n string) *agg {
			if per[n] == nil {
				per[n] = &agg{}
				nodes = append(nodes, n)
			}
			return per[n]
		}
		for _, it := range items {
			a := get(it.NodeID)
			if it.Delete {
				a.del++
			} else {
				a.keep++
				a.keepB += it.Bytes
			}
		}
		for _, it := range objs {
			a := get(it.NodeID)
			a.delObj++
			a.delB += it.Bytes
		}
		sort.Strings(nodes)

		keepNote := fmt.Sprintf("keep %d per node", p.RetentionKeep)
		if p.RetentionKeep <= 0 {
			keepNote = "keep everything"
		}
		fmt.Fprintf(tw, "\nPLAN %s (%s), bucket %s, %s\n", p.Name, p.ID, p.S3Bucket, keepNote)
		fmt.Fprintln(tw, "NODE\tKEEP ITEMS\tKEEP BYTES\tDELETE ITEMS\tDELETE OBJECTS\tDELETE BYTES")
		var tot agg
		for _, n := range nodes {
			a := per[n]
			name := n
			if h := hosts[n]; h != "" {
				name = h + " (" + n + ")"
			}
			fmt.Fprintf(tw, "%s\t%d\t%s\t%d\t%d\t%s\n", name, a.keep, humanBytes(uint64(a.keepB)), a.del, a.delObj, humanBytes(uint64(a.delB)))
			tot.keep += a.keep
			tot.keepB += a.keepB
			tot.del += a.del
			tot.delObj += a.delObj
			tot.delB += a.delB
		}
		fmt.Fprintf(tw, "TOTAL\t%d\t%s\t%d\t%d\t%s\n", tot.keep, humanBytes(uint64(tot.keepB)), tot.del, tot.delObj, humanBytes(uint64(tot.delB)))

		if listBucket {
			if err := r.reportBucket(ctx, tw, p, items); err != nil {
				fmt.Fprintf(tw, "bucket listing failed: %v\n", err)
			}
		}
	}

	cutoff := time.Now().Add(-failedRunTTL).Unix()
	var expItems, expRuns int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_run_items i JOIN backup_runs r ON r.id = i.run_id
		WHERE i.status <> 'success' AND i.started_at < ? AND r.status NOT IN ('pending', 'running')`, cutoff).Scan(&expItems)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_runs r WHERE started_at < ?
		AND status NOT IN ('pending', 'running', 'success')
		AND NOT EXISTS (SELECT 1 FROM backup_run_items i WHERE i.run_id = r.id)`, cutoff).Scan(&expRuns)
	fmt.Fprintf(tw, "\nFailed items older than 30 days (rows only): %d; item-less failed runs older than 30 days: %d\n", expItems, expRuns)
	fmt.Fprintln(tw, "Dry run: nothing was deleted.")
	return tw.Flush()
}

// reportBucket lists the static prefix of the plan's key template and reports
// objects no row references and rows whose object is missing.
func (r *Runner) reportBucket(ctx context.Context, w io.Writer, p plan, items []retentionItem) error {
	cfg, err := r.loadS3Endpoint(ctx, p.S3EndpointID)
	if err != nil {
		return fmt.Errorf("s3 endpoint: %w", err)
	}
	client, err := s3.NewClient(ctx, s3.EndpointConfig{
		Endpoint: cfg.Endpoint, Region: cfg.Region, PathStyle: cfg.PathStyle,
		Creds: s3.Credentials{AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey},
	})
	if err != nil {
		return err
	}
	tmpl := p.KeyTemplate
	if tmpl == "" {
		tmpl = renderKeyTemplate("", AgentInfo{}, p, time.Time{})
	}
	prefix, _, _ := strings.Cut(tmpl, "{")
	objs, err := client.ListAllObjects(ctx, p.S3Bucket, prefix)
	if err != nil {
		return err
	}
	// Every row of every plan counts as a reference, so a shared prefix does
	// not turn another plan's archives into orphans.
	rows, err := r.db.QueryContext(ctx, `SELECT COALESCE(s3_key, ''), COALESCE(extra_s3_keys, '[]') FROM backup_run_items`)
	if err != nil {
		return err
	}
	referenced := map[string]bool{}
	for rows.Next() {
		var k, extra string
		if rows.Scan(&k, &extra) == nil {
			referenced[k] = true
			var more []string
			_ = json.Unmarshal([]byte(extra), &more)
			for _, m := range more {
				referenced[m] = true
			}
		}
	}
	rows.Close()

	inBucket := map[string]bool{}
	var orphanN, orphanB, totalB int64
	var examples []string
	for _, o := range objs {
		inBucket[o.Key] = true
		totalB += o.Size
		if !referenced[o.Key] {
			orphanN++
			orphanB += o.Size
			if len(examples) < 20 {
				examples = append(examples, o.Key)
			}
		}
	}
	missing := 0
	for _, it := range items {
		if it.Key != "" && !inBucket[it.Key] {
			missing++
		}
	}
	fmt.Fprintf(w, "Bucket prefix %q: %d objects, %s; unreferenced (orphans, never deleted): %d, %s; success rows whose object is missing: %d\n",
		prefix, len(objs), humanBytes(uint64(totalB)), orphanN, humanBytes(uint64(orphanB)), missing)
	for _, k := range examples {
		fmt.Fprintf(w, "  orphan: %s\n", k)
	}
	return nil
}
