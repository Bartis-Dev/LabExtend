package leader

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
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
	if req.NodeID == "" || req.Schedule == "" || req.Command == "" {
		writeErr(w, http.StatusBadRequest, errors.New("node_id + schedule + command required"))
		return
	}
	if req.RunAs == "" {
		req.RunAs = "root"
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
	d.applyToAgent(r, req.NodeID)
	d.Audit.Log(r.Context(), r, "cron.create", "cronjob", id, map[string]any{"node": req.NodeID, "schedule": req.Schedule})
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (d *CronDeps) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req cronReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if nodeID != "" {
		d.applyToAgent(r, nodeID)
	}
	d.Audit.Log(r.Context(), r, "cron.update", "cronjob", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (d *CronDeps) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var nodeID string
	_ = d.DB.QueryRowContext(r.Context(), `SELECT node_id FROM cronjobs WHERE id = ?`, id).Scan(&nodeID)
	if _, err := d.DB.ExecContext(r.Context(), `DELETE FROM cronjobs WHERE id = ?`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if nodeID != "" {
		d.applyToAgent(r, nodeID)
	}
	d.Audit.Log(r.Context(), r, "cron.delete", "cronjob", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Apply pushes the current entries for one node to its agent (also called
// from frontend "force apply" button).
func (d *CronDeps) Apply(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	d.applyToAgent(r, nodeID)
	d.Audit.Log(r.Context(), r, "cron.apply", "node", nodeID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// applyToAgent fetches all cronjobs for nodeID and asks the agent to apply them.
// Best-effort: agent offline → log + skip; agent will pick up on next manual Apply.
func (d *CronDeps) applyToAgent(r *http.Request, nodeID string) {
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		return
	}
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT id, schedule, command, run_as, comment, enabled
		FROM cronjobs WHERE node_id = ?
	`, nodeID)
	if err != nil {
		return
	}
	defer rows.Close()
	var entries []*pb.CronEntry
	for rows.Next() {
		var e pb.CronEntry
		var enabled int
		if err := rows.Scan(&e.Id, &e.Schedule, &e.Command, &e.User, &e.Comment, &enabled); err != nil {
			continue
		}
		e.Enabled = enabled == 1
		entries = append(entries, &e)
	}
	_, _ = conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_ApplyCron{ApplyCron: &pb.ApplyCronReq{Entries: entries}},
	})
}
