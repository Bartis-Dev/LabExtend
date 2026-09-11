package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// CronDeps groups what the cron handlers need.
type CronDeps struct {
	DB       *sql.DB
	Registry *AgentRegistry
	Audit    *AuditLogger

	// applyMu serializes pushes to agents. Every push carries the node's whole
	// job set as read from the DB, so two concurrent pushes can arrive in the
	// wrong order and the older snapshot silently drops the newer job from the
	// host while it still shows in the UI.
	// ponytail: one lock for all nodes; per-node locks if cron edits ever get busy.
	applyMu sync.Mutex
}

// CronJob is the JSON shape.
type CronJob struct {
	ID        string `json:"id"`
	NodeID    string `json:"node_id"`
	Schedule  string `json:"schedule"`
	Command   string `json:"command"`
	RunAs     string `json:"run_as"`
	Comment   string `json:"comment"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

func (d *CronDeps) List(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	q := `SELECT id, node_id, schedule, command, run_as, comment, enabled, created_at, updated_at FROM cronjobs`
	args := []any{}
	if nodeID != "" {
		q += ` WHERE node_id = ?`
		args = append(args, nodeID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := d.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []CronJob{}
	for rows.Next() {
		var c CronJob
		var enabled int
		if err := rows.Scan(&c.ID, &c.NodeID, &c.Schedule, &c.Command, &c.RunAs,
			&c.Comment, &enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		c.Enabled = enabled == 1
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

type cronReq struct {
	NodeID   string `json:"node_id"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	RunAs    string `json:"run_as"`
	Comment  string `json:"comment"`
	Enabled  bool   `json:"enabled"`
}

func (d *CronDeps) Create(w http.ResponseWriter, r *http.Request) {
	var req cronReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.NodeID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("node_id required"))
		return
	}
	if req.RunAs == "" {
		req.RunAs = "root"
	}
	if err := validateCronJob(req.Schedule, req.Command, req.RunAs); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id := uuid.NewString()
	now := time.Now().Unix()
	if _, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO cronjobs (id, node_id, schedule, command, run_as, comment, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, req.NodeID, req.Schedule, req.Command, req.RunAs, req.Comment,
		boolI(req.Enabled), now, now); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	applyErr := d.applyToAgent(r.Context(), req.NodeID)
	d.Audit.Log(r.Context(), r, "cron.create", "cronjob", id, map[string]any{"node": req.NodeID, "schedule": req.Schedule})
	writeJSON(w, http.StatusOK, withApplyResult(map[string]any{"id": id}, applyErr))
}

func (d *CronDeps) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req cronReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.RunAs == "" {
		req.RunAs = "root"
	}
	if err := validateCronJob(req.Schedule, req.Command, req.RunAs); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := d.DB.ExecContext(r.Context(), `
		UPDATE cronjobs SET schedule = ?, command = ?, run_as = ?, comment = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, req.Schedule, req.Command, req.RunAs, req.Comment, boolI(req.Enabled), time.Now().Unix(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("job not found"))
		return
	}
	// Need node_id for re-apply; look it up.
	var nodeID string
	_ = d.DB.QueryRowContext(r.Context(), `SELECT node_id FROM cronjobs WHERE id = ?`, id).Scan(&nodeID)
	var applyErr error
	if nodeID != "" {
		applyErr = d.applyToAgent(r.Context(), nodeID)
	}
	d.Audit.Log(r.Context(), r, "cron.update", "cronjob", id, nil)
	writeJSON(w, http.StatusOK, withApplyResult(map[string]any{"updated": n}, applyErr))
}

func (d *CronDeps) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var nodeID string
	_ = d.DB.QueryRowContext(r.Context(), `SELECT node_id FROM cronjobs WHERE id = ?`, id).Scan(&nodeID)
	if _, err := d.DB.ExecContext(r.Context(), `DELETE FROM cronjobs WHERE id = ?`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var applyErr error
	if nodeID != "" {
		applyErr = d.applyToAgent(r.Context(), nodeID)
	}
	d.Audit.Log(r.Context(), r, "cron.delete", "cronjob", id, nil)
	writeJSON(w, http.StatusOK, withApplyResult(map[string]any{"ok": true}, applyErr))
}

// Apply pushes the current entries for one node to its agent (also called
// from frontend "force apply" button).
func (d *CronDeps) Apply(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	if err := d.applyToAgent(r.Context(), nodeID); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	d.Audit.Log(r.Context(), r, "cron.apply", "node", nodeID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// withApplyResult reports whether the host actually picked the change up. The
// job is stored either way, so this is not an HTTP error — but "saved" and
// "running on the host" are different claims, and the UI has to be able to tell
// them apart.
func withApplyResult(body map[string]any, applyErr error) map[string]any {
	body["applied"] = applyErr == nil
	if applyErr != nil {
		body["apply_error"] = applyErr.Error()
	}
	return body
}

// applyToAgent pushes every job stored for nodeID to its agent, which rewrites
// /etc/cron.d/bpm from scratch. The DB is the source of truth, so a failure
// here means the host runs an older job set than the UI shows — callers must
// surface it instead of reporting success.
func (d *CronDeps) applyToAgent(ctx context.Context, nodeID string) error {
	d.applyMu.Lock()
	defer d.applyMu.Unlock()

	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		return err
	}
	entries, err := d.loadCronEntries(ctx, nodeID)
	if err != nil {
		return err
	}
	if _, err := conn.RequestWithDefault(ctx, &pb.Command{
		Op: &pb.Command_ApplyCron{ApplyCron: &pb.ApplyCronReq{Entries: entries}},
	}); err != nil {
		return err
	}
	return nil
}

func (d *CronDeps) loadCronEntries(ctx context.Context, nodeID string) ([]*pb.CronEntry, error) {
	rows, err := d.DB.QueryContext(ctx, `
		SELECT id, schedule, command, run_as, comment, enabled
		FROM cronjobs WHERE node_id = ?
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("load jobs: %w", err)
	}
	defer rows.Close()
	var entries []*pb.CronEntry
	for rows.Next() {
		var e pb.CronEntry
		var enabled int
		if err := rows.Scan(&e.Id, &e.Schedule, &e.Command, &e.User, &e.Comment, &enabled); err != nil {
			// Skipping the row would push a job set without it, which deletes
			// that job from the host.
			return nil, fmt.Errorf("load jobs: %w", err)
		}
		e.Enabled = enabled == 1
		entries = append(entries, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load jobs: %w", err)
	}
	return entries, nil
}

var (
	// One crontab time field: numbers, names (jan, mon) and the * / , - operators.
	cronFieldRe = regexp.MustCompile(`^[0-9A-Za-z*/,\-]+$`)
	cronUserRe  = regexp.MustCompile(`^[a-z_][a-z0-9_-]*\$?$`)
)

// validateCronJob mirrors the agent's validateCronEntry. The agent rewrites
// the node's whole cron file and refuses the write if ANY entry is invalid, so
// a job the leader stored but the agent rejects does not just fail on its own:
// it freezes every other job on that node at its previous state, for as long
// as the bad row stays in the DB.
func validateCronJob(schedule, command, runAs string) error {
	if strings.ContainsAny(schedule, "\r\n") || strings.ContainsAny(command, "\r\n") {
		return errors.New("schedule and command must be a single line")
	}
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return fmt.Errorf("schedule needs 5 fields (minute hour day month weekday), got %d in %q", len(fields), schedule)
	}
	for _, f := range fields {
		if !cronFieldRe.MatchString(f) {
			return fmt.Errorf("invalid schedule field %q", f)
		}
	}
	if strings.TrimSpace(command) == "" {
		return errors.New("command required")
	}
	if !cronUserRe.MatchString(runAs) {
		return fmt.Errorf("invalid user %q", runAs)
	}
	return nil
}
