package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Bartis-Dev/LabExtend/internal/backup"
)

// BackupDeps groups what the backup-API handlers need.
type BackupDeps struct {
	DB        *sql.DB
	Scheduler *backup.Scheduler
	Audit     *AuditLogger
}

// BackupPlan is the JSON shape.
type BackupPlan struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Sources          []string `json:"sources"`
	ScopeType        string   `json:"scope_type"`
	ScopeValue       string   `json:"scope_value"`
	S3EndpointID     string   `json:"s3_endpoint_id"`
	S3Bucket         string   `json:"s3_bucket"`
	KeyTemplate      string   `json:"key_template"`
	Schedule         string   `json:"schedule"`
	RetentionKeep    int      `json:"retention_keep"`
	Compression      string   `json:"compression"`
	CompressionLevel int      `json:"compression_level"`
	WebhookID        string   `json:"webhook_id,omitempty"`
	WebhookMode      string   `json:"webhook_mode"`
	Engine           string   `json:"engine"`            // tar (default) | pgdump
	VerifyRestore    bool     `json:"verify_restore"`    // pgdump only
	Enabled          bool     `json:"enabled"`
	CreatedAt        int64    `json:"created_at"`
	UpdatedAt        int64    `json:"updated_at"`
	LastRunAt        int64    `json:"last_run_at,omitempty"`
	NextRunAt        int64    `json:"next_run_at,omitempty"`
}

// ListPlans returns every plan.
func (d *BackupDeps) ListPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT id, name, sources_json, scope_type, COALESCE(scope_value,''),
		       s3_endpoint_id, s3_bucket, key_template, schedule,
		       retention_keep, compression, compression_level,
		       COALESCE(webhook_id,''), webhook_mode, engine, verify_restore, enabled,
		       created_at, updated_at,
		       COALESCE(last_run_at, 0), COALESCE(next_run_at, 0)
		FROM backup_plans ORDER BY name
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []BackupPlan{}
	for rows.Next() {
		var p BackupPlan
		var sourcesJSON string
		var en, verifyInt int
		if err := rows.Scan(&p.ID, &p.Name, &sourcesJSON, &p.ScopeType, &p.ScopeValue,
			&p.S3EndpointID, &p.S3Bucket, &p.KeyTemplate, &p.Schedule,
			&p.RetentionKeep, &p.Compression, &p.CompressionLevel,
			&p.WebhookID, &p.WebhookMode, &p.Engine, &verifyInt, &en,
			&p.CreatedAt, &p.UpdatedAt, &p.LastRunAt, &p.NextRunAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		_ = json.Unmarshal([]byte(sourcesJSON), &p.Sources)
		p.Enabled = en == 1
		p.VerifyRestore = verifyInt == 1
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": out})
}

type planReq struct {
	Name             string   `json:"name"`
	Sources          []string `json:"sources"`
	ScopeType        string   `json:"scope_type"`
	ScopeValue       string   `json:"scope_value"`
	S3EndpointID     string   `json:"s3_endpoint_id"`
	S3Bucket         string   `json:"s3_bucket"`
	KeyTemplate      string   `json:"key_template"`
	Schedule         string   `json:"schedule"`
	RetentionKeep    int      `json:"retention_keep"`
	Compression      string   `json:"compression"`
	CompressionLevel int      `json:"compression_level"`
	WebhookID        string   `json:"webhook_id"`
	WebhookMode      string   `json:"webhook_mode"`
	Engine           string   `json:"engine"`
	VerifyRestore    bool     `json:"verify_restore"`
	Enabled          bool     `json:"enabled"`
}

func (d *BackupDeps) CreatePlan(w http.ResponseWriter, r *http.Request) {
	var req planReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || len(req.Sources) == 0 || req.S3EndpointID == "" ||
		req.S3Bucket == "" || req.Schedule == "" {
		writeErr(w, http.StatusBadRequest,
			errors.New("name + sources + s3_endpoint_id + s3_bucket + schedule required"))
		return
	}
	if req.ScopeType == "" {
		req.ScopeType = "all"
	}
	if req.Compression == "" {
		req.Compression = "gzip"
	}
	if req.RetentionKeep == 0 {
		req.RetentionKeep = 7
	}
	if req.WebhookMode == "" {
		req.WebhookMode = "on-error"
	}
	if req.Engine == "" {
		req.Engine = "tar"
	}
	if req.KeyTemplate == "" {
		if req.Engine == "pgdump" {
			req.KeyTemplate = "backups/{host}/{date}/{plan}.dump"
		} else {
			req.KeyTemplate = "backups/{host}/{date}/{plan}.tar.gz"
		}
	}
	sourcesJSON, _ := json.Marshal(req.Sources)
	id := uuid.NewString()
	now := time.Now().Unix()

	var widArg any
	if req.WebhookID != "" {
		widArg = req.WebhookID
	}
	var scopeArg any
	if req.ScopeValue != "" {
		scopeArg = req.ScopeValue
	}

	if _, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO backup_plans
			(id, name, sources_json, scope_type, scope_value, s3_endpoint_id,
			 s3_bucket, key_template, schedule, retention_keep, compression,
			 compression_level, webhook_id, webhook_mode, engine, verify_restore,
			 enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, req.Name, string(sourcesJSON), req.ScopeType, scopeArg,
		req.S3EndpointID, req.S3Bucket, req.KeyTemplate, req.Schedule,
		req.RetentionKeep, req.Compression, req.CompressionLevel,
		widArg, req.WebhookMode, req.Engine, boolI(req.VerifyRestore),
		boolI(req.Enabled), now, now); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if d.Scheduler != nil {
		_ = d.Scheduler.Refresh(r.Context())
	}
	d.Audit.Log(r.Context(), r, "backup.plan.create", "backup_plan", id, map[string]any{"name": req.Name})
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (d *BackupDeps) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req planReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sourcesJSON, _ := json.Marshal(req.Sources)
	var widArg any
	if req.WebhookID != "" {
		widArg = req.WebhookID
	}
	var scopeArg any
	if req.ScopeValue != "" {
		scopeArg = req.ScopeValue
	}
	if req.Engine == "" {
		req.Engine = "tar"
	}
	res, err := d.DB.ExecContext(r.Context(), `
		UPDATE backup_plans SET
			name = ?, sources_json = ?, scope_type = ?, scope_value = ?,
			s3_endpoint_id = ?, s3_bucket = ?, key_template = ?, schedule = ?,
			retention_keep = ?, compression = ?, compression_level = ?,
			webhook_id = ?, webhook_mode = ?, engine = ?, verify_restore = ?,
			enabled = ?, updated_at = ?
		WHERE id = ?
	`, req.Name, string(sourcesJSON), req.ScopeType, scopeArg,
		req.S3EndpointID, req.S3Bucket, req.KeyTemplate, req.Schedule,
		req.RetentionKeep, req.Compression, req.CompressionLevel,
		widArg, req.WebhookMode, req.Engine, boolI(req.VerifyRestore),
		boolI(req.Enabled), time.Now().Unix(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("plan not found"))
		return
	}
	if d.Scheduler != nil {
		_ = d.Scheduler.Refresh(r.Context())
	}
	d.Audit.Log(r.Context(), r, "backup.plan.update", "backup_plan", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (d *BackupDeps) DeletePlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.DB.ExecContext(r.Context(), `DELETE FROM backup_plans WHERE id = ?`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if d.Scheduler != nil {
		_ = d.Scheduler.Refresh(r.Context())
	}
	d.Audit.Log(r.Context(), r, "backup.plan.delete", "backup_plan", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *BackupDeps) TriggerPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor := actorEmail(r)
	if d.Scheduler == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("scheduler not started"))
		return
	}
	runID, err := d.Scheduler.Trigger(r.Context(), id, "manual:"+actor)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "backup.plan.trigger", "backup_plan", id, map[string]any{"run_id": runID})
	writeJSON(w, http.StatusOK, map[string]any{"run_id": runID})
}

// BackupRun is the JSON shape for /api/backups/runs.
type BackupRun struct {
	ID           string             `json:"id"`
	PlanID       string             `json:"plan_id"`
	PlanName     string             `json:"plan_name"`
	TriggeredBy  string             `json:"triggered_by"`
	StartedAt    int64              `json:"started_at"`
	FinishedAt   int64              `json:"finished_at,omitempty"`
	Status       string             `json:"status"`
	ErrorSummary string             `json:"error_summary,omitempty"`
	Items        []backup.NodeResult `json:"items,omitempty"`
}

func (d *BackupDeps) ListRuns(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	planID := r.URL.Query().Get("plan_id")
	q := `
		SELECT br.id, br.plan_id, COALESCE(bp.name,''), br.triggered_by,
		       br.started_at, COALESCE(br.finished_at,0), br.status,
		       COALESCE(br.error_summary,''), COALESCE(br.log_excerpt,'')
		FROM backup_runs br LEFT JOIN backup_plans bp ON bp.id = br.plan_id
		WHERE 1=1`
	args := []any{}
	if planID != "" {
		q += ` AND br.plan_id = ?`
		args = append(args, planID)
	}
	q += ` ORDER BY br.started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []BackupRun{}
	for rows.Next() {
		var b BackupRun
		var excerpt string
		if err := rows.Scan(&b.ID, &b.PlanID, &b.PlanName, &b.TriggeredBy,
			&b.StartedAt, &b.FinishedAt, &b.Status, &b.ErrorSummary, &excerpt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if excerpt != "" {
			_ = json.Unmarshal([]byte(excerpt), &b.Items)
		}
		out = append(out, b)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

// actorEmail returns the email of the current user from the request context.
func actorEmail(r *http.Request) string {
	if sess, _ := r.Context().Value(authCtxKey{}).(*authSession); sess != nil {
		return sess.email
	}
	return "unknown"
}

// authSession is a tiny shim — the real session lives in auth pkg but we
// don't have easy access to .email there. The runner falls back to "unknown".
type authSession struct{ email string }

var _ context.Context // keep import alive
