package backup

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Bartis-Dev/LabExtend/internal/discord"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
	"github.com/Bartis-Dev/LabExtend/internal/notify"
)

// AgentRequester is the slim interface the runner needs from leader. We
// don't import internal/leader (would be cyclic); leader provides this via
// the wireup in run.go.
type AgentRequester interface {
	// ListAgents returns (id, labels) for every connected agent.
	ListAgents() []AgentInfo
	// Request sends a Command to a specific agent and blocks for the result.
	Request(ctx context.Context, agentID string, cmd *pb.Command) (*pb.CommandResult, error)
}

// AgentInfo is the slice of AgentConn fields the runner cares about.
type AgentInfo struct {
	ID       string
	Hostname string
	Labels   map[string]string
}

// Publisher pushes backup.* events to SSE subscribers (the leader's Hub).
type Publisher interface {
	Publish(topic string, data any)
}

// Runner orchestrates one plan execution: resolve scope → fan out gRPC
// commands to each in-scope agent → collect per-node results → prune
// retention → emit backup.failed / post the success summary.
type Runner struct {
	db         *sql.DB
	agents     AgentRequester
	pub        Publisher
	secretsKey string // hex AES-256-GCM key for decrypting S3 + webhook creds
	events     *notify.Center

	// inflight holds the plans that currently have a run executing. Nothing
	// prevented a plan from running twice at once: RunPlan starts a goroutine
	// and returns immediately, so the cron job function always completes
	// instantly and even cron.SkipIfStillRunning would not have helped. Two
	// concurrent runs of the same plan stream two different archives into the
	// same key (the default template has date granularity), and both record
	// success with different checksums, at most one of which matches the object
	// that survived.
	inflightMu sync.Mutex
	inflight   map[string]struct{}
}

// ErrPlanRunning reports that a plan was asked to start while one of its runs
// is still executing.
var ErrPlanRunning = errors.New("a run for this plan is already in progress")

func NewRunner(db *sql.DB, agents AgentRequester, pub Publisher, secretsKey string, events *notify.Center) *Runner {
	return &Runner{
		db: db, agents: agents, pub: pub, secretsKey: secretsKey, events: events,
		inflight: make(map[string]struct{}),
	}
}

// claim reserves planID for a run, reporting false if one is already active.
func (r *Runner) claim(planID string) bool {
	r.inflightMu.Lock()
	defer r.inflightMu.Unlock()
	if _, busy := r.inflight[planID]; busy {
		return false
	}
	r.inflight[planID] = struct{}{}
	return true
}

func (r *Runner) release(planID string) {
	r.inflightMu.Lock()
	delete(r.inflight, planID)
	r.inflightMu.Unlock()
}

// RunPlan executes one full backup run. Returns the new run_id immediately
// and runs the actual work asynchronously (since a backup can take minutes).
// Caller can poll /api/backups/runs/:id to follow progress.
func (r *Runner) RunPlan(_ context.Context, planID, planName, triggeredBy string) (string, error) {
	if !r.claim(planID) {
		return "", ErrPlanRunning
	}
	runID := uuid.NewString()
	go func() {
		defer r.release(planID)
		r.run(planID, planName, runID, triggeredBy)
	}()
	return runID, nil
}

// run is the async worker.
func (r *Runner) run(planID, planName, runID, triggeredBy string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	started := time.Now()

	// Load the plan.
	plan, err := r.loadPlan(ctx, planID)
	if err != nil {
		slog.Error("backup: load plan failed", "plan", planID, "err", err)
		return
	}

	// Insert backup_runs row with status=pending.
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO backup_runs (id, plan_id, triggered_by, started_at, status)
		VALUES (?, ?, ?, ?, 'pending')
	`, runID, planID, triggeredBy, started.Unix()); err != nil {
		slog.Error("backup: insert run failed", "err", err)
		return
	}

	r.publish("backup.started", map[string]any{
		"run_id": runID, "plan_id": planID, "plan_name": planName,
		"triggered_by": triggeredBy, "started_at": started.Unix(),
	})

	// Resolve scope.
	targets, missing := r.resolveScope(ctx, plan)
	if len(targets) == 0 && len(missing) == 0 {
		const e = "no agents matched the scope"
		r.markFinished(ctx, runID, "failed", e, nil)
		r.notify(ctx, plan, "failed", e, planName, runID, 0, time.Since(started), nil)
		return
	}

	// Mark running.
	_, _ = r.db.ExecContext(ctx, `UPDATE backup_runs SET status = 'running' WHERE id = ?`, runID)

	// Resolve S3 endpoint creds.
	s3Cfg, err := r.loadS3Endpoint(ctx, plan.S3EndpointID)
	if err != nil {
		e := "s3 endpoint: " + err.Error()
		r.markFinished(ctx, runID, "failed", e, nil)
		r.notify(ctx, plan, "failed", e, planName, runID, 0, time.Since(started), nil)
		return
	}

	// Fan-out: one goroutine per target, each calls RunBackup on its agent.
	var wg sync.WaitGroup
	results := make([]NodeResult, 0, len(targets)+len(missing))
	resMu := sync.Mutex{}

	// A known node that is not connected is a node without tonight's backup.
	// Skipping it silently reported such a run as success.
	for _, m := range missing {
		results = append(results, r.failMissingNode(ctx, runID, m))
	}

	for _, t := range targets {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := r.runOneNode(ctx, plan, t, runID, s3Cfg)
			resMu.Lock()
			results = append(results, res)
			resMu.Unlock()
		}()
	}
	wg.Wait()

	// Aggregate status.
	var totalBytes uint64
	var failures int
	for _, n := range results {
		totalBytes += n.BytesUploaded
		if n.Status != "success" {
			failures++
		}
	}
	status := "success"
	switch {
	case failures == len(results):
		status = "failed"
	case failures > 0:
		status = "partial"
	}

	r.markFinished(ctx, runID, status, "", results)

	// A run that failed on every node uploaded nothing that could make an
	// older archive redundant.
	if plan.RetentionKeep > 0 && failures < len(results) {
		r.applyRetention(ctx, plan, runID, s3Cfg)
	}
	if err := cleanupExpired(context.WithoutCancel(ctx), r.db, time.Now()); err != nil {
		slog.Warn("backup: removing expired failed runs failed", "err", err)
	}

	r.notify(ctx, plan, status, "", planName, runID, totalBytes, time.Since(started), results)

	r.publish("backup.finished", map[string]any{
		"run_id": runID, "plan_id": planID, "status": status,
		"bytes": totalBytes, "duration_ms": time.Since(started).Milliseconds(),
		"failures": failures, "total": len(results),
	})
}

// runOneNode dispatches RunBackup to one agent and records the per-node row.
func (r *Runner) runOneNode(ctx context.Context, plan plan, target AgentInfo, runID string, s3 s3EndpointCfg) NodeResult {
	res := NodeResult{NodeID: target.ID, Status: "running"}

	startedAt := time.Now().Unix()
	itemID := r.insertRunItem(ctx, runID, target.ID, startedAt)

	r.publish("backup.node.started", map[string]any{
		"run_id": runID, "node_id": target.ID,
	})

	s3Key := renderKeyTemplate(plan.KeyTemplate, target, plan, time.Now())

	reply, err := r.agents.Request(ctx, target.ID, &pb.Command{
		Op: &pb.Command_RunBackup{RunBackup: &pb.RunBackupReq{
			PlanId:        plan.ID,
			RunId:         runID,
			Sources:       plan.Sources,
			S3Endpoint:    s3.Endpoint,
			S3Region:      s3.Region,
			S3Bucket:      plan.S3Bucket,
			S3Key:         s3Key,
			S3AccessKey:   s3.AccessKey,
			S3SecretKey:   s3.SecretKey,
			S3PathStyle:   s3.PathStyle,
			Compression:   plan.Compression,
			Level:         uint32(plan.CompressionLevel),
			Engine:        plan.Engine,
			VerifyRestore: plan.VerifyRestore,
		}},
	})

	finishedAt := time.Now().Unix()

	if err != nil {
		res.Status = "failed"
		res.Error = err.Error()
		r.updateRunItem(ctx, itemID, "failed", finishedAt, 0, 0, "", res.Error, s3Key, nil)
		r.publish("backup.node.finished", map[string]any{
			"run_id": runID, "node_id": target.ID, "status": "failed", "error": res.Error,
		})
		return res
	}

	rb := reply.GetRunBackup()
	// Agents that predate s3_keys report the one object in s3_key.
	keys := rb.GetS3Keys()
	if len(keys) == 0 {
		keys = []string{rb.GetS3Key()}
	}
	res.Status = "success"
	res.S3Key = keys[0]
	if len(keys) > 1 {
		res.S3Keys = keys
	}
	res.BytesUploaded = rb.GetBytesUploaded()
	res.FileCount = rb.GetFileCount()
	res.SHA256 = rb.GetSha256()
	r.updateRunItem(ctx, itemID, "success", finishedAt, res.BytesUploaded, res.FileCount, res.SHA256, "", keys[0], keys[1:])

	r.publish("backup.node.finished", map[string]any{
		"run_id": runID, "node_id": target.ID, "status": "success",
		"bytes": res.BytesUploaded, "files": res.FileCount, "key": res.S3Key,
	})
	return res
}

// failMissingNode records a failed item for an in-scope node whose agent is
// not connected.
func (r *Runner) failMissingNode(ctx context.Context, runID string, n AgentInfo) NodeResult {
	now := time.Now().Unix()
	res := NodeResult{NodeID: n.ID, Status: "failed", Error: "agent not connected (node " + n.Hostname + " is offline)"}
	itemID := r.insertRunItem(ctx, runID, n.ID, now)
	r.updateRunItem(ctx, itemID, "failed", now, 0, 0, "", res.Error, "", nil)
	r.publish("backup.node.finished", map[string]any{
		"run_id": runID, "node_id": n.ID, "status": "failed", "error": res.Error,
	})
	return res
}

// ─── DB helpers ─────────────────────────────────────────────────────────────

type plan struct {
	ID               string
	Name             string
	Sources          []string
	ScopeType        string // node | all | label
	ScopeValue       string
	S3EndpointID     string
	S3Bucket         string
	KeyTemplate      string
	RetentionKeep    int
	Compression      string
	CompressionLevel int
	WebhookID        string
	WebhookMode      string
	Engine           string // tar (default) | pgdump
	VerifyRestore    bool
}

type s3EndpointCfg struct {
	Endpoint  string
	Region    string
	PathStyle bool
	AccessKey string
	SecretKey string
}

func (r *Runner) loadPlan(ctx context.Context, planID string) (plan, error) {
	var p plan
	var sourcesJSON string
	var scopeValue sql.NullString
	var webhookID sql.NullString
	var verifyInt int
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, sources_json, scope_type, scope_value, s3_endpoint_id,
		       s3_bucket, key_template, retention_keep, compression, compression_level,
		       webhook_id, webhook_mode, engine, verify_restore
		FROM backup_plans WHERE id = ?
	`, planID).Scan(&p.ID, &p.Name, &sourcesJSON, &p.ScopeType, &scopeValue,
		&p.S3EndpointID, &p.S3Bucket, &p.KeyTemplate, &p.RetentionKeep,
		&p.Compression, &p.CompressionLevel, &webhookID, &p.WebhookMode,
		&p.Engine, &verifyInt)
	p.VerifyRestore = verifyInt == 1
	if err != nil {
		return p, err
	}
	_ = json.Unmarshal([]byte(sourcesJSON), &p.Sources)
	if scopeValue.Valid {
		p.ScopeValue = scopeValue.String
	}
	if webhookID.Valid {
		p.WebhookID = webhookID.String
	}
	return p, nil
}

func (r *Runner) loadS3Endpoint(ctx context.Context, id string) (s3EndpointCfg, error) {
	var c s3EndpointCfg
	var encSecret string
	var ps int
	if err := r.db.QueryRowContext(ctx, `
		SELECT endpoint, region, access_key, secret_key, path_style
		FROM s3_endpoints WHERE id = ?
	`, id).Scan(&c.Endpoint, &c.Region, &c.AccessKey, &encSecret, &ps); err != nil {
		return c, err
	}
	c.PathStyle = ps == 1
	plain, err := decryptAESGCM(r.secretsKey, encSecret)
	if err != nil {
		return c, fmt.Errorf("decrypt secret_key: %w", err)
	}
	c.SecretKey = plain
	return c, nil
}

// resolveScope returns the connected agents in the plan's scope, and the
// nodes known from the nodes table that match the scope but are not connected.
func (r *Runner) resolveScope(ctx context.Context, p plan) (targets, missing []AgentInfo) {
	targets = matchScope(p, r.agents.ListAgents())
	connected := make(map[string]bool, len(targets))
	for _, a := range r.agents.ListAgents() {
		connected[a.ID] = true
	}
	for _, n := range matchScope(p, r.knownNodes(ctx)) {
		if !connected[n.ID] {
			missing = append(missing, n)
		}
	}
	return targets, missing
}

// knownNodes lists every node the leader has ever seen, connected or not.
func (r *Runner) knownNodes(ctx context.Context) []AgentInfo {
	rows, err := r.db.QueryContext(ctx, `SELECT id, hostname, COALESCE(labels_json, '{}') FROM nodes`)
	if err != nil {
		slog.Warn("backup: listing known nodes failed, offline nodes are not reported", "err", err)
		return nil
	}
	defer rows.Close()
	var out []AgentInfo
	for rows.Next() {
		var n AgentInfo
		var host sql.NullString
		var labels string
		if err := rows.Scan(&n.ID, &host, &labels); err != nil {
			slog.Warn("backup: scanning known node failed", "err", err)
			continue
		}
		n.Hostname = host.String
		_ = json.Unmarshal([]byte(labels), &n.Labels)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("backup: listing known nodes failed", "err", err)
	}
	return out
}

func matchScope(p plan, all []AgentInfo) []AgentInfo {
	switch p.ScopeType {
	case "all":
		return all
	case "node":
		// Comma-separated list of node IDs or hostnames. Allows the UI to
		// offer a multiselect and still encode the result in one column.
		wanted := make(map[string]bool)
		for _, w := range strings.Split(p.ScopeValue, ",") {
			w = strings.TrimSpace(w)
			if w != "" {
				wanted[w] = true
			}
		}
		out := make([]AgentInfo, 0)
		for _, a := range all {
			if wanted[a.ID] || wanted[a.Hostname] {
				out = append(out, a)
			}
		}
		return out
	case "label":
		k, v, ok := strings.Cut(p.ScopeValue, "=")
		if !ok {
			return nil
		}
		out := make([]AgentInfo, 0)
		for _, a := range all {
			if a.Labels[k] == v {
				out = append(out, a)
			}
		}
		return out
	}
	return nil
}

func (r *Runner) insertRunItem(ctx context.Context, runID, nodeID string, startedAt int64) int64 {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO backup_run_items (run_id, node_id, started_at, status)
		VALUES (?, ?, ?, 'running')
	`, runID, nodeID, startedAt)
	if err != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

func (r *Runner) updateRunItem(ctx context.Context, itemID int64, status string, finishedAt int64,
	bytes uint64, files uint32, sha string, errMsg, s3Key string, extraKeys []string) {
	if extraKeys == nil {
		extraKeys = []string{}
	}
	extra, _ := json.Marshal(extraKeys)
	_, err := r.db.ExecContext(ctx, `
		UPDATE backup_run_items
		   SET status = ?, finished_at = ?, bytes_uploaded = ?, file_count = ?,
		       sha256 = ?, error = ?, s3_key = ?, extra_s3_keys = ?
		 WHERE id = ?
	`, status, finishedAt, bytes, files, sha, errMsg, s3Key, string(extra), itemID)
	if err != nil {
		slog.Warn("backup: update item failed", "err", err)
	}
}

func (r *Runner) markFinished(ctx context.Context, runID, status, errSummary string, results []NodeResult) {
	// The run's own context is already past its deadline whenever the run hit
	// the 4h timeout — exactly the case where a terminal status matters most.
	// Writing it on that dead context leaves the row on 'running' forever, and
	// DeleteRun refuses to remove in-flight runs, so it can never be cleared.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	excerpt := ""
	if len(results) > 0 {
		// Truncate the per-node error strings, not the encoded array. Cutting
		// the JSON at a byte offset produced a document that never parses
		// again, so the reader (handlers_backup.go) silently ended up with an
		// empty item list: a partial run showed no detail at all and the S3
		// keys of the archives that did land became unreachable.
		trimmed := make([]NodeResult, len(results))
		copy(trimmed, results)
		for i := range trimmed {
			if len(trimmed[i].Error) > 512 {
				trimmed[i].Error = trimmed[i].Error[:512] + "… (truncated)"
			}
		}
		b, err := json.Marshal(trimmed)
		if err != nil {
			slog.Warn("backup: encoding run results failed", "run", runID, "err", err)
		} else {
			excerpt = string(b)
		}
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE backup_runs
		   SET finished_at = ?, status = ?, error_summary = ?, log_excerpt = ?
		 WHERE id = ?
	`, time.Now().Unix(), status, errSummary, excerpt, runID); err != nil {
		slog.Error("backup: writing terminal run status failed",
			"run", runID, "status", status, "err", err)
	}
}

// ReconcileStaleRuns fails every run still marked in-flight. Called once at
// leader startup: a run whose leader died mid-flight has no one left to write
// its terminal status, and such a row is neither deletable nor cleanable
// through the API.
func (r *Runner) ReconcileStaleRuns(ctx context.Context) {
	type stale struct{ runID, planName string }
	var runs []stale
	// Only runs that started before this call: the scheduler may already have
	// started one of this leader's own.
	rows, err := r.db.QueryContext(ctx, `
		SELECT br.id, COALESCE(bp.name, br.plan_id)
		  FROM backup_runs br LEFT JOIN backup_plans bp ON bp.id = br.plan_id
		 WHERE br.status IN ('pending', 'running') AND br.started_at <= ?`, time.Now().Unix())
	if err != nil {
		slog.Warn("backup: listing stale runs failed", "err", err)
		return
	}
	for rows.Next() {
		var s stale
		if rows.Scan(&s.runID, &s.planName) == nil {
			runs = append(runs, s)
		}
	}
	_ = rows.Close()
	if len(runs) == 0 {
		return
	}

	var failed int
	for _, s := range runs {
		res, err := r.db.ExecContext(ctx, `
			UPDATE backup_runs
			   SET status = 'failed',
			       finished_at = ?,
			       error_summary = 'leader restarted while the run was in flight'
			 WHERE id = ? AND status IN ('pending', 'running')
		`, time.Now().Unix(), s.runID)
		if err != nil {
			slog.Warn("backup: reconciling stale run failed", "run", s.runID, "err", err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		failed++
		r.emitFailed(ctx, s.planName, s.runID, "failed", "leader restarted while the run was in flight", nil)
	}
	if failed > 0 {
		slog.Warn("backup: marked stale in-flight runs as failed", "count", failed)
	}
}

func (r *Runner) publish(topic string, data any) {
	if r.pub != nil {
		r.pub.Publish(topic, data)
	}
}

// ─── webhook ────────────────────────────────────────────────────────────────

// notify announces a finished run: a failed or partial run becomes a
// backup.failed event for every subscribed webhook and the panel, a success is
// posted only to the plan's own webhook and only with webhook_mode=always.
//
// It exists as its own method because run() can end in four places: three
// early failures that never touch a node, and the normal path at the bottom.
// Only the bottom used to notify, so a run that died while resolving its S3
// credentials was written to backup_runs as failed and announced nowhere.
// Six consecutive nightly runs were lost that way in August 2026 under a plan
// explicitly set to webhook_mode=on-error - the one setting whose entire
// purpose is to make that impossible.
//
// errSummary carries the reason for the early failures, which have no
// NodeResults to derive one from; it is empty on the normal path.
func (r *Runner) notify(ctx context.Context, p plan, status, errSummary, planName, runID string,
	bytes uint64, dur time.Duration, results []NodeResult) {
	if status != "success" {
		r.emitFailed(ctx, planName, runID, status, errSummary, results)
		return
	}
	if p.WebhookID != "" && p.WebhookMode == "always" {
		r.postSuccess(ctx, p, planName, runID, bytes, dur, results)
	}
}

// emitFailed raises backup.failed for a failed or partial run.
func (r *Runner) emitFailed(ctx context.Context, planName, runID, status, errSummary string, results []NodeResult) {
	title, severity := "Backup failed · "+planName, notify.SeverityCritical
	if status == "partial" {
		title, severity = "Backup partially failed · "+planName, notify.SeverityWarning
	}
	var lines []string
	if errSummary != "" {
		lines = append(lines, truncate(errSummary, 1000))
	}
	failedNodes := []string{}
	for _, n := range results {
		if n.Status != "success" {
			failedNodes = append(failedNodes, n.NodeID)
			// 400 chars per node still shows the AWS error code
			// (AccessDenied / SignatureDoesNotMatch), which always sits past
			// the first 100.
			lines = append(lines, n.NodeID+": "+truncate(n.Error, 400))
		}
	}
	if len(results) > 0 {
		lines = append(lines, fmt.Sprintf("%d of %d nodes failed.", len(failedNodes), len(results)))
	}
	r.events.Emit(ctx, notify.Event{
		Type: notify.BackupFailed, Severity: severity, Title: title,
		Body: strings.Join(lines, "\n"),
		Link: "/backups?run=" + runID,
		Details: map[string]any{
			"plan_name": planName, "run_id": runID, "status": status,
			"failed_nodes": failedNodes, "error": errSummary,
		},
	})
}

func (r *Runner) postSuccess(ctx context.Context, p plan, planName, runID string,
	bytes uint64, dur time.Duration, results []NodeResult) {
	// Same reason as markFinished: the run context may be past its deadline.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	var url string
	if err := r.db.QueryRowContext(ctx,
		`SELECT url FROM webhook_configs WHERE id = ? AND enabled = 1`, p.WebhookID).Scan(&url); err != nil {
		slog.Warn("backup: webhook url lookup failed", "err", err)
		return
	}
	fields := []discord.Field{
		{Name: "Run", Value: runID, Inline: false},
		{Name: "Bytes", Value: humanBytes(bytes), Inline: true},
		{Name: "Duration", Value: dur.Round(time.Second).String(), Inline: true},
		{Name: "Nodes", Value: fmt.Sprintf("%d", len(results)), Inline: true},
	}
	if err := discord.NewClient(url).Post(ctx,
		discord.BackupSummary(planName, runID, "success", fields, "labextend", time.Now())); err != nil {
		slog.Warn("backup: webhook post failed", "plan", p.ID, "err", err)
	}
}

// renderKeyTemplate substitutes placeholders in the S3 key template.
// Supported: {date}, {datetime}, {host}, {node_id}, {plan}, {plan_id}.
//
// Substituted values are passed through s3KeySafe — S3 allows almost any
// byte in a key but providers' SigV4 signers do NOT all handle the same
// special chars the same way. The classic killer is ':' in a key path:
// Cloudflare R2 rejects the request with `403 AccessDenied` (instead of
// a clearer SignatureDoesNotMatch) when the path contains a raw colon.
// Plan names like "Docker-Volumes-Daily-03:00" explode silently without
// this. We normalize *substituted values only* — the template itself
// (your slashes between segments) is honored as written.
func renderKeyTemplate(tmpl string, t AgentInfo, p plan, now time.Time) string {
	r := strings.NewReplacer(
		"{date}", now.Format("2006-01-02"),
		"{datetime}", now.Format("2006-01-02T15-04-05"),
		"{host}", s3KeySafe(t.Hostname),
		"{node_id}", s3KeySafe(t.ID),
		"{plan}", s3KeySafe(p.Name),
		"{plan_id}", s3KeySafe(p.ID),
	)
	if tmpl == "" {
		// pgdump default uses .dump (postgres custom format), tar default uses .tar.gz
		if p.Engine == "pgdump" {
			tmpl = "backups/{host}/{date}/{plan}.dump"
		} else {
			tmpl = "backups/{host}/{date}/{plan}.tar.gz"
		}
	}
	return r.Replace(tmpl)
}

// s3KeySafe normalizes a string for safe embedding in an S3 object key
// segment. Keeps alphanumerics, dot, dash, underscore. Everything else
// becomes a dash (collapsing runs of dashes to a single one). Per AWS
// docs these are the "safe" characters that every S3-compatible
// implementation handles identically.
func s3KeySafe(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	// Trim leading/trailing dashes.
	return strings.Trim(b.String(), "-")
}

func humanBytes(n uint64) string {
	const k = 1024
	if n < k {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	v := float64(n) / k
	i := 0
	for v >= k && i < len(units)-1 {
		v /= k
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// RunResult is the JSON shape callers see (legacy alias for backwards-compat).
type RunResult struct {
	RunID         string
	PlanID        string
	Status        string
	Duration      time.Duration
	NodeResults   []NodeResult
	BytesTotal    uint64
	FailedSummary string
}

// NodeResult is one row of the per-node outcome for a run.
type NodeResult struct {
	NodeID        string   `json:"node_id"`
	Status        string   `json:"status"`
	S3Key         string   `json:"s3_key,omitempty"`
	S3Keys        []string `json:"s3_keys,omitempty"` // every object, when there are several
	BytesUploaded uint64   `json:"bytes_uploaded"`
	FileCount     uint32   `json:"file_count"`
	SHA256        string   `json:"sha256,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// ─── AES-GCM decrypt (mirrors handlers_s3.encrypt format) ──────────────────

func decryptAESGCM(hexKey, encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", err
	}
	if len(key) != 32 {
		return "", errors.New("key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
