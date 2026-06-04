package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/go-chi/chi/v5"
)

// ctxAlias is a tiny rename so handler signatures stay short. (context.Context)
type ctxAlias = context.Context

// AuditLogger appends a row to audit_log per state-changing action. The
// schema is in 0001_initial.sql.
type AuditLogger struct {
	db      *sql.DB
	disable bool
}

func newAuditLogger(db *sql.DB, disable bool) *AuditLogger {
	return &AuditLogger{db: db, disable: disable}
}

// Log appends one row. Best-effort: errors are logged, not returned, because
// audit must never fail the underlying operation.
func (a *AuditLogger) Log(ctx context.Context, r *http.Request, action, targetKind, targetID string, details map[string]any) {
	if a == nil || a.disable || a.db == nil {
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
//	?limit=100  ?action=file.chown  ?target_kind=file
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

// clientIPFromRequest extracts a sensible client IP.
func clientIPFromRequest(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	addr := r.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

// chi helper unused outside route definitions but keep the import alive.
var _ = chi.URLParam
