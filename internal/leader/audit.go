package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/go-chi/chi/v5"
)

// ctxAlias is a tiny rename so handler signatures stay short. (context.Context)
type ctxAlias = context.Context

// Audit modes, stored in system_state under auditModeKey.
const (
	auditModeKey  = "audit_mode"
	auditModeAuto = "auto" // full log with 2+ users, last auditSoloKeep rows with one
	auditModeOn   = "on"
	auditModeOff  = "off"

	auditSoloKeep = 100
)

// AuditLogger appends a row to audit_log per state-changing action. The
// schema is in 0001_initial.sql.
type AuditLogger struct {
	db *sql.DB

	mu   sync.RWMutex
	mode string
}

func newAuditLogger(db *sql.DB) *AuditLogger {
	return &AuditLogger{db: db, mode: auditModeAuto}
}

// auditState is what the current mode resolves to.
type auditState struct {
	Mode    string `json:"mode"`
	Enabled bool   `json:"enabled"`
	Keep    int    `json:"keep"` // 0 = keep everything
	Users   int    `json:"users"`
}

// Load reads the stored mode and applies it once, so switching to a
// single-user instance trims the existing log right at startup.
func (a *AuditLogger) Load(ctx context.Context) error {
	var v string
	err := a.db.QueryRowContext(ctx, `SELECT value FROM system_state WHERE key = ?`, auditModeKey).Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("load audit mode: %w", err)
	}
	if validAuditMode(v) {
		a.mu.Lock()
		a.mode = v
		a.mu.Unlock()
	}
	return a.trim(ctx)
}

func validAuditMode(m string) bool {
	return m == auditModeAuto || m == auditModeOn || m == auditModeOff
}

// SetMode persists a new mode and applies it immediately.
func (a *AuditLogger) SetMode(ctx context.Context, mode string) error {
	if !validAuditMode(mode) {
		return fmt.Errorf("audit_mode must be auto, on or off")
	}
	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO system_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, auditModeKey, mode); err != nil {
		return fmt.Errorf("save audit mode: %w", err)
	}
	a.mu.Lock()
	a.mode = mode
	a.mu.Unlock()
	return a.trim(ctx)
}

// State resolves the mode against the current user count.
func (a *AuditLogger) State(ctx context.Context) auditState {
	a.mu.RLock()
	st := auditState{Mode: a.mode}
	a.mu.RUnlock()
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&st.Users)
	switch st.Mode {
	case auditModeOff:
	case auditModeOn:
		st.Enabled = true
	default:
		st.Enabled = true
		if st.Users < 2 {
			st.Keep = auditSoloKeep
		}
	}
	return st
}

// trim drops everything but the newest Keep rows when the mode asks for it.
func (a *AuditLogger) trim(ctx context.Context) error {
	st := a.State(ctx)
	if st.Keep == 0 {
		return nil
	}
	if _, err := a.db.ExecContext(ctx, `
		DELETE FROM audit_log
		WHERE id <= (SELECT id FROM audit_log ORDER BY id DESC LIMIT 1 OFFSET ?)`, st.Keep); err != nil {
		return fmt.Errorf("trim audit log: %w", err)
	}
	return nil
}

// Log appends one row. Best-effort: errors are logged, not returned, because
// audit must never fail the underlying operation.
func (a *AuditLogger) Log(ctx context.Context, r *http.Request, action, targetKind, targetID string, details map[string]any) {
	if a == nil || a.db == nil {
		return
	}
	st := a.State(ctx)
	if !st.Enabled {
		return
	}
	var (
		actorID    sql.NullInt64
		actorEmail sql.NullString
	)
	if sess, _ := ctx.Value(authCtxKey{}).(*auth.Session); sess != nil {
		actorID = sql.NullInt64{Int64: sess.UserID, Valid: true}
		// best-effort email lookup (cheap, runs in audit goroutine)
		var em string
		_ = a.db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = ?`, sess.UserID).Scan(&em)
		if em != "" {
			actorEmail = sql.NullString{String: em, Valid: true}
		}
	}
	ip := ""
	if r != nil {
		ip = clientIPFromRequest(r)
	}
	detailsJSON := "{}"
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			detailsJSON = string(b)
		}
	}
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO audit_log (ts, actor_user_id, actor_email, source_ip, action, target_kind, target_id, details_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, time.Now().Unix(), actorID, actorEmail, ip, action, targetKind, targetID, detailsJSON)
	if err != nil {
		slog.Warn("audit log insert failed", "err", err, "action", action)
		return
	}
	if st.Keep > 0 {
		if err := a.trim(ctx); err != nil {
			slog.Warn("audit log trim failed", "err", err)
		}
	}
}

// AuditDeps wraps the logger + db for the read-side handlers.
type AuditDeps struct {
	DB *sql.DB
}

// AuditRow is the JSON shape returned by /api/audit.
type AuditRow struct {
	ID         int64           `json:"id"`
	TS         int64           `json:"ts"`
	ActorID    int64           `json:"actor_id,omitempty"`
	ActorEmail string          `json:"actor_email,omitempty"`
	SourceIP   string          `json:"source_ip,omitempty"`
	Action     string          `json:"action"`
	TargetKind string          `json:"target_kind,omitempty"`
	TargetID   string          `json:"target_id,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
}

// List returns the audit log with optional filters.
//
//	?limit=100  ?action=node_path.create  ?target_kind=file
func (d *AuditDeps) List(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	q := `
		SELECT id, ts, COALESCE(actor_user_id,0), COALESCE(actor_email,''),
		       COALESCE(source_ip,''), action, COALESCE(target_kind,''),
		       COALESCE(target_id,''), details_json
		FROM audit_log
		WHERE 1=1`
	args := []any{}
	if v := r.URL.Query().Get("action"); v != "" {
		q += ` AND action = ?`
		args = append(args, v)
	}
	if v := r.URL.Query().Get("target_kind"); v != "" {
		q += ` AND target_kind = ?`
		args = append(args, v)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []AuditRow{}
	for rows.Next() {
		var a AuditRow
		var details string
		if err := rows.Scan(&a.ID, &a.TS, &a.ActorID, &a.ActorEmail, &a.SourceIP,
			&a.Action, &a.TargetKind, &a.TargetID, &details); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if details != "" {
			a.Details = json.RawMessage(details)
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": out})
}

// clientIPFromRequest extracts the client IP from RemoteAddr.
//
// It deliberately does not look at X-Forwarded-For: the realIP middleware has
// already folded that header into RemoteAddr, but only when the peer is a
// configured trusted proxy, and stripped it otherwise. Reading it a second time
// here would reinstate exactly the spoofing the middleware exists to prevent —
// every audit row's source_ip was attacker-chosen while this did so.
func clientIPFromRequest(r *http.Request) string {
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// chi helper unused outside route definitions but keep the import alive.
var _ = chi.URLParam
