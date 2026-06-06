package leader

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	if req.IsAdmin != nil {
		_, _ = d.DB.ExecContext(r.Context(),
			`UPDATE users SET is_admin = ?, updated_at = ? WHERE id = ?`, boolI(*req.IsAdmin), now, id)
	}
	if req.IsActive != nil {
		_, _ = d.DB.ExecContext(r.Context(),
			`UPDATE users SET is_active = ?, updated_at = ? WHERE id = ?`, boolI(*req.IsActive), now, id)
		// If disabling, revoke all sessions.
		if !*req.IsActive {
			_, _ = d.DB.ExecContext(r.Context(),
				`DELETE FROM sessions WHERE user_id = ?`, id)
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
		_, _ = d.DB.ExecContext(r.Context(),
			`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, now, id)
	}

	d.Audit.Log(r.Context(), r, "user.update", "user", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *UsersDeps) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Refuse to delete the last admin.
	var adminCount int
	_ = d.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND is_active = 1`).Scan(&adminCount)
	var isAdmin int
	_ = d.DB.QueryRowContext(r.Context(),
		`SELECT is_admin FROM users WHERE id = ?`, id).Scan(&isAdmin)
	if adminCount <= 1 && isAdmin == 1 {
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
