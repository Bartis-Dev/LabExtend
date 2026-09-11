package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
)

// UsersDeps groups everything the admin user-CRUD handlers need.
type UsersDeps struct {
	DB    *sql.DB
	Audit *AuditLogger
}

// UserView is the JSON shape (no password hash on the wire).
type UserView struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	IsActive    bool   `json:"is_active"`
	HasTOTP     bool   `json:"has_totp"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	LastLoginAt int64  `json:"last_login_at,omitempty"`
}

func (d *UsersDeps) List(w http.ResponseWriter, r *http.Request) {
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT u.id, u.email, COALESCE(u.username,''), u.display_name, u.is_admin, u.is_active,
		       u.created_at, u.updated_at, COALESCE(u.last_login_at, 0),
		       COALESCE((SELECT enabled FROM totp_secrets WHERE user_id = u.id), 0)
		FROM users u ORDER BY u.created_at
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []UserView{}
	for rows.Next() {
		var v UserView
		var admin, active, totp int
		if err := rows.Scan(&v.ID, &v.Email, &v.Username, &v.DisplayName, &admin, &active,
			&v.CreatedAt, &v.UpdatedAt, &v.LastLoginAt, &totp); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		v.IsAdmin = admin == 1
		v.IsActive = active == 1
		v.HasTOTP = totp == 1
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type createUserReq struct {
	Email       string `json:"email"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

func (d *UsersDeps) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Email == "" || len(req.Password) < 12 {
		writeErr(w, http.StatusBadRequest, errors.New("email + password (≥12 chars) required"))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now().Unix()
	var usernameArg any
	if u := strings.TrimSpace(req.Username); u != "" {
		usernameArg = u
	}
	res, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO users (email, username, display_name, password_hash, is_admin, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
	`, req.Email, usernameArg, req.DisplayName, hash, boolI(req.IsAdmin), now, now)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, _ := res.LastInsertId()
	d.Audit.Log(r.Context(), r, "user.create", "user", strconv.FormatInt(id, 10),
		map[string]any{"email": req.Email, "username": req.Username, "is_admin": req.IsAdmin})
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

type updateUserReq struct {
	DisplayName *string `json:"display_name"`
	Username    *string `json:"username"`
	IsAdmin     *bool   `json:"is_admin"`
	IsActive    *bool   `json:"is_active"`
	NewPassword string  `json:"new_password"`
}

func (d *UsersDeps) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	now := time.Now().Unix()

	if req.DisplayName != nil {
		_, _ = d.DB.ExecContext(r.Context(),
			`UPDATE users SET display_name = ?, updated_at = ? WHERE id = ?`, *req.DisplayName, now, id)
	}
	if req.Username != nil {
		var arg any
		if u := strings.TrimSpace(*req.Username); u != "" {
			arg = u
		}
		if _, err := d.DB.ExecContext(r.Context(),
			`UPDATE users SET username = ?, updated_at = ? WHERE id = ?`, arg, now, id); err != nil {
			writeErr(w, http.StatusBadRequest, err) // most likely UNIQUE violation
			return
		}
	}
	// Demoting or deactivating the only remaining admin leaves nobody who can
	// reach /api/users — requireAdmin re-reads the flag per request — and the
	// setup wizard is closed, so recovery means editing the SQLite file on the
	// volume by hand. Delete already refused this; the other two paths did not.
	if (req.IsAdmin != nil && !*req.IsAdmin) || (req.IsActive != nil && !*req.IsActive) {
		last, err := isLastActiveAdmin(r.Context(), d.DB, id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if last {
			writeErr(w, http.StatusBadRequest, errors.New("refusing to demote or deactivate the last admin"))
			return
		}
	}

	if req.IsAdmin != nil {
		if _, err := d.DB.ExecContext(r.Context(),
			`UPDATE users SET is_admin = ?, updated_at = ? WHERE id = ?`, boolI(*req.IsAdmin), now, id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if req.IsActive != nil {
		if _, err := d.DB.ExecContext(r.Context(),
			`UPDATE users SET is_active = ?, updated_at = ? WHERE id = ?`, boolI(*req.IsActive), now, id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// If disabling, revoke all sessions.
		if !*req.IsActive {
			if _, err := d.DB.ExecContext(r.Context(),
				`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
	}
	if req.NewPassword != "" {
		if len(req.NewPassword) < 12 {
			writeErr(w, http.StatusBadRequest, errors.New("password must be ≥12 chars"))
			return
		}
		hash, err := auth.HashPassword(req.NewPassword)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// A discarded error here answered 200 while the password was unchanged.
		if _, err := d.DB.ExecContext(r.Context(),
			`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, now, id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// An admin reset is usually a response to a compromise, so the old
		// sessions must not survive it.
		if _, err := d.DB.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			slog.Error("revoking sessions after admin password reset failed", "user", id, "err", err)
		}
	}

	d.Audit.Log(r.Context(), r, "user.update", "user", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// isLastActiveAdmin reports whether id is the only account left that can still
// administer the instance. Phrased as "does anyone else qualify" rather than
// counting and comparing, because the old form ignored the target's own
// is_active flag: with one active and one deactivated admin it refused to
// delete the deactivated one, and it silently allowed the delete whenever the
// count query failed (the discarded Scan error left adminCount at 0).
func isLastActiveAdmin(ctx context.Context, db *sql.DB, id string) (bool, error) {
	var others int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users
		 WHERE is_admin = 1 AND is_active = 1 AND id != ?
	`, id).Scan(&others); err != nil {
		return false, err
	}
	if others > 0 {
		return false, nil
	}
	var isAdmin, isActive int
	err := db.QueryRowContext(ctx,
		`SELECT is_admin, is_active FROM users WHERE id = ?`, id).Scan(&isAdmin, &isActive)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return isAdmin == 1 && isActive == 1, nil
}

func (d *UsersDeps) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	last, err := isLastActiveAdmin(r.Context(), d.DB, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if last {
		writeErr(w, http.StatusBadRequest, errors.New("refusing to delete the last admin"))
		return
	}
	res, err := d.DB.ExecContext(r.Context(), `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "user.delete", "user", id, nil)
	n, _ := res.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}
