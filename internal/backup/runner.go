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
// retention → fire webhook.
type Runner struct {
	db         *sql.DB
	agents     AgentRequester
	pub        Publisher
	secretsKey string // hex AES-256-GCM key for decrypting S3 + webhook creds
}

func NewRunner(db *sql.DB, agents AgentRequester, pub Publisher, secretsKey string) *Runner {
	return &Runner{db: db, agents: agents, pub: pub, secretsKey: secretsKey}
}

// RunPlan executes one full backup run. Returns the new run_id immediately
// and runs the actual work asynchronously (since a backup can take minutes).
// Caller can poll /api/backups/runs/:id to follow progress.
func (r *Runner) RunPlan(ctx context.Context, planID, planName, triggeredBy string) string {
	runID := uuid.NewString()
	go r.run(planID, planName, runID, triggeredBy)
	return runID
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
	targets := r.resolveScope(plan)
	if len(targets) == 0 {
		r.markFinished(ctx, runID, "failed", "no agents matched the scope", nil)
		return
	}

	// Mark running.
	_, _ = r.db.ExecContext(ctx, `UPDATE backup_runs SET status = 'running' WHERE id = ?`, runID)

	// Resolve S3 endpoint creds.
	s3Cfg, err := r.loadS3Endpoint(ctx, plan.S3EndpointID)
	if err != nil {
		r.markFinished(ctx, runID, "failed", "s3 endpoint: "+err.Error(), nil)
		return
	}

	// Fan-out: one goroutine per target, each calls RunBackup on its agent.
	var wg sync.WaitGroup
	results := make([]NodeResult, 0, len(targets))
	resMu := sync.Mutex{}

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

	// Retention prune (best-effort).
	if plan.RetentionKeep > 0 {
		if err := r.pruneRetention(ctx, plan, s3Cfg); err != nil {
			slog.Warn("backup: retention prune failed", "plan", planID, "err", err)
		}
	}

	// Webhook (if configured + matches mode).
	if plan.WebhookID != "" {
		fire := false
		switch plan.WebhookMode {
		case "always":
			fire = true
		case "on-error":
			fire = status != "success"
		}
		if fire {
			r.fireWebhook(ctx, plan, status, planName, runID, totalBytes, time.Since(started), results)
		}
	}

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
			PlanId:      plan.ID,
			RunId:       runID,
			Sources:     plan.Sources,
			S3Endpoint:  s3.Endpoint,
			S3Region:    s3.Region,
			S3Bucket:    plan.S3Bucket,
			S3Key:       s3Key,
			S3AccessKey: s3.AccessKey,
			S3SecretKey: s3.SecretKey,
			S3PathStyle: s3.PathStyle,
			Compression: plan.Compression,
			Level:       uint32(plan.CompressionLevel),
		}},
	})

	finishedAt := time.Now().Unix()

	if err != nil {
		res.Status = "failed"
		res.Error = err.Error()
		r.updateRunItem(ctx, itemID, "failed", finishedAt, 0, 0, "", res.Error, s3Key)
		r.publish("backup.node.finished", map[string]any{
			"run_id": runID, "node_id": target.ID, "status": "failed", "error": res.Error,
		})
		return res
	}

	rb := reply.GetRunBackup()
	res.Status = "success"
	res.S3Key = rb.S3Key
	res.BytesUploaded = rb.BytesUploaded
	res.FileCount = rb.FileCount
	res.SHA256 = rb.Sha256
	r.updateRunItem(ctx, itemID, "success", finishedAt, rb.BytesUploaded, rb.FileCount, rb.Sha256, "", rb.S3Key)

	r.publish("backup.node.finished", map[string]any{
		"run_id": runID, "node_id": target.ID, "status": "success",
		"bytes": rb.BytesUploaded, "files": rb.FileCount, "key": rb.S3Key,
	})
	return res
}

// ─── retention prune ────────────────────────────────────────────────────────

// pruneRetention lists keys matching this plan's prefix per node and deletes
// all but the newest N. Implemented via the s3 package (we re-use the leader's
// client construction via a small inline call).
func (r *Runner) pruneRetention(_ context.Context, _ plan, _ s3EndpointCfg) error {
	// NOTE: full impl requires listing under each node's key prefix.
	// For now this is a placeholder hook; logging-only.
	// TODO: list, sort by LastModified, delete oldest beyond keep.
	return nil
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
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, sources_json, scope_type, scope_value, s3_endpoint_id,
		       s3_bucket, key_template, retention_keep, compression, compression_level,
		       webhook_id, webhook_mode
		FROM backup_plans WHERE id = ?
	`, planID).Scan(&p.ID, &p.Name, &sourcesJSON, &p.ScopeType, &scopeValue,
		&p.S3EndpointID, &p.S3Bucket, &p.KeyTemplate, &p.RetentionKeep,
		&p.Compression, &p.CompressionLevel, &webhookID, &p.WebhookMode)
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

func (r *Runner) resolveScope(p plan) []AgentInfo {
	all := r.agents.ListAgents()
	switch p.ScopeType {
	case "all":
		return all
	case "node":
		for _, a := range all {
			if a.ID == p.ScopeValue || a.Hostname == p.ScopeValue {
				return []AgentInfo{a}
			}
		}
		return nil
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
	bytes uint64, files uint32, sha string, errMsg, s3Key string) {
	_, err := r.db.ExecContext(ctx, `
		UPDATE backup_run_items
		   SET status = ?, finished_at = ?, bytes_uploaded = ?, file_count = ?,
		       sha256 = ?, error = ?, s3_key = ?
		 WHERE id = ?
	`, status, finishedAt, bytes, files, sha, errMsg, s3Key, itemID)
	if err != nil {
		slog.Warn("backup: update item failed", "err", err)
	}
}

func (r *Runner) markFinished(ctx context.Context, runID, status, errSummary string, results []NodeResult) {
	excerpt := ""
	if len(results) > 0 {
		b, _ := json.Marshal(results)
		excerpt = string(b)
		if len(excerpt) > 8192 {
			excerpt = excerpt[:8192]
		}
	}
	_, _ = r.db.ExecContext(ctx, `
		UPDATE backup_runs
		   SET finished_at = ?, status = ?, error_summary = ?, log_excerpt = ?
		 WHERE id = ?
	`, time.Now().Unix(), status, errSummary, excerpt, runID)
}

func (r *Runner) publish(topic string, data any) {
	if r.pub != nil {
		r.pub.Publish(topic, data)
	}
}

// ─── webhook ────────────────────────────────────────────────────────────────

func (r *Runner) fireWebhook(ctx context.Context, p plan, status, planName, runID string,
	bytes uint64, dur time.Duration, results []NodeResult) {
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
	if status != "success" {
		var failed []string
		for _, n := range results {
			if n.Status != "success" {
				failed = append(failed, n.NodeID+": "+truncate(n.Error, 100))
			}
		}
		if len(failed) > 0 {
			fields = append(fields, discord.Field{Name: "Failures", Value: strings.Join(failed, "\n")})
		}
	}
	c := discord.NewClient(url)
	if err := c.Post(context.Background(),
		discord.BackupSummary(planName, runID, status, fields, "labextend", time.Now())); err != nil {
		slog.Warn("backup: webhook post failed", "err", err)
	}
}

// renderKeyTemplate substitutes placeholders in the S3 key template.
// Supported: {date}, {datetime}, {host}, {node_id}, {plan}, {plan_id}.
func renderKeyTemplate(tmpl string, t AgentInfo, p plan, now time.Time) string {
	r := strings.NewReplacer(
		"{date}", now.Format("2006-01-02"),
		"{datetime}", now.Format("2006-01-02T15-04-05"),
		"{host}", t.Hostname,
		"{node_id}", t.ID,
		"{plan}", p.Name,
		"{plan_id}", p.ID,
	)
	if tmpl == "" {
		tmpl = "backups/{host}/{date}/{plan}.tar.gz"
	}
	return r.Replace(tmpl)
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
	NodeID        string `json:"node_id"`
	Status        string `json:"status"`
	S3Key         string `json:"s3_key,omitempty"`
	BytesUploaded uint64 `json:"bytes_uploaded"`
	FileCount     uint32 `json:"file_count"`
	SHA256        string `json:"sha256,omitempty"`
	Error         string `json:"error,omitempty"`
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
