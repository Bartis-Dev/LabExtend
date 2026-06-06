package leader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
)

// authCtxKey is the request-context key for the *auth.Session attached by
// the session middleware.
type authCtxKey struct{}

// userInfo is the JSON shape returned by /api/me and /api/auth/login.
// Requires2FA=true is the only signal that means "your session is half-
// authenticated; POST a TOTP code to /api/auth/2fa/verify before doing
// anything else". When false, the session is fully authenticated.
type userInfo struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	CSRFToken   string `json:"csrf_token"`
	Requires2FA bool   `json:"requires_2fa,omitempty"`
}

// AuthDeps groups the pieces every auth/setup handler needs.
type AuthDeps struct {
	DB       *sql.DB
	Sessions *auth.SessionStore
	TOTP     *auth.TOTPManager
}

// Verify2FA accepts a TOTP / recovery code on a session that's currently
// is_2fa_pending=1. On success, flips the flag so subsequent requests pass
// requireAuth.
func (d *AuthDeps) Verify2FA(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("no session"))
		return
	}
	if !sess.Is2FAPending {
		writeErr(w, http.StatusBadRequest, errors.New("session is not 2FA-pending"))
		return
	}
	var body struct {
		Code      string `json:"code"`
		IsRecover bool   `json:"is_recovery"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if d.TOTP == nil {
		writeErr(w, http.StatusInternalServerError, errors.New("totp manager not configured"))
		return
	}
	var verr error
	if body.IsRecover {
		verr = d.TOTP.VerifyRecoveryCode(r.Context(), sess.UserID, body.Code)
	} else {
		verr = d.TOTP.Verify(r.Context(), sess.UserID, body.Code)
	}
	if verr != nil {
		writeErr(w, http.StatusUnauthorized, errors.New("invalid 2fa code"))
		return
	}
	if err := d.Sessions.PromoteFrom2FA(r.Context(), sess); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, userInfo{
		ID:          sess.UserID,
		CSRFToken:   sess.CSRFToken,
	})
}

// ─── setup wizard ───────────────────────────────────────────────────────────

// SetupStatus reports whether the first-run wizard has been completed.
func (d *AuthDeps) SetupStatus(w http.ResponseWriter, r *http.Request) {
	done, err := isSetupComplete(r.Context(), d.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"setup_completed": done})
}

type setupInitReq struct {
	Email       string `json:"email"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// SetupInitialize creates the first admin user. Refuses if setup has already
// been completed.
func (d *AuthDeps) SetupInitialize(w http.ResponseWriter, r *http.Request) {
	done, err := isSetupComplete(r.Context(), d.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if done {
		writeErr(w, http.StatusConflict, errors.New("setup already completed"))
		return
	}

	var req setupInitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Email == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, errors.New("email + password required"))
		return
	}
	if len(req.Password) < 12 {
		writeErr(w, http.StatusBadRequest, errors.New("password must be at least 12 characters"))
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
		INSERT INTO users
			(email, username, display_name, password_hash, is_admin, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, 1, ?, ?)
	`, req.Email, usernameArg, req.DisplayName, hash, now, now)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("create user: %w", err))
		return
	}
	userID, _ := res.LastInsertId()

	if _, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO system_state (key, value) VALUES ('setup_completed', 'true')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	sess, err := d.Sessions.Create(r.Context(), w, r, userID, false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	slog.Info("setup completed", "user_id", userID, "email", req.Email)

	writeJSON(w, http.StatusOK, userInfo{
		ID:          userID,
		Email:       req.Email,
		Username:    strings.TrimSpace(req.Username),
		DisplayName: req.DisplayName,
		IsAdmin:     true,
		CSRFToken:   sess.CSRFToken,
	})
}

// ─── login / logout / me ────────────────────────────────────────────────────

type loginReq struct {
	// Identifier accepts either the user's email or their username.
	// `Email` is the legacy field name (older clients posted this); both are
	// merged into one lookup against email OR username.
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

// Login validates credentials, creates a session, and returns the user info.
// Returns 401 on invalid credentials; never reveals whether the identifier exists.
func (d *AuthDeps) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ident := strings.TrimSpace(req.Identifier)
	if ident == "" {
		ident = strings.TrimSpace(req.Email)
	}
	if ident == "" {
		writeErr(w, http.StatusBadRequest, errors.New("identifier required"))
		return
	}

	var (
		id       int64
		email    string
		username sql.NullString
		display  string
		hash     string
		isAdmin  int
		isActive int
		hasTOTP  int
	)
	err := d.DB.QueryRowContext(r.Context(), `
		SELECT u.id, u.email, u.username, u.display_name, u.password_hash, u.is_admin, u.is_active,
		       COALESCE((SELECT enabled FROM totp_secrets WHERE user_id = u.id), 0)
		FROM users u
		WHERE u.email = ? OR u.username = ?
		LIMIT 1
	`, ident, ident).Scan(&id, &email, &username, &display, &hash, &isAdmin, &isActive, &hasTOTP)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusUnauthorized, errors.New("invalid credentials"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if isActive != 1 {
		writeErr(w, http.StatusForbidden, errors.New("account disabled"))
		return
	}

	ok, err := auth.VerifyPassword(req.Password, hash)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, errors.New("invalid credentials"))
		return
	}

	sess, err := d.Sessions.Create(r.Context(), w, r, id, hasTOTP == 1)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Update last_login_at (best effort)
	_, _ = d.DB.ExecContext(r.Context(),
		`UPDATE users SET last_login_at = ? WHERE id = ?`, time.Now().Unix(), id)

	writeJSON(w, http.StatusOK, userInfo{
		ID:          id,
		Email:       email,
		Username:    username.String,
		DisplayName: display,
		IsAdmin:     isAdmin == 1,
		CSRFToken:   sess.CSRFToken,
		Requires2FA: sess.Is2FAPending,
	})
}

// Logout destroys the current session and clears the cookie.
func (d *AuthDeps) Logout(w http.ResponseWriter, r *http.Request) {
	_ = d.Sessions.Destroy(r.Context(), w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Me returns the current authenticated user's profile. Requires session
// middleware to have populated context.
func (d *AuthDeps) Me(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("no session"))
		return
	}

	var (
		email    string
		username sql.NullString
		display  string
		isAdmin  int
	)
	err := d.DB.QueryRowContext(r.Context(),
		`SELECT email, username, display_name, is_admin FROM users WHERE id = ?`, sess.UserID,
	).Scan(&email, &username, &display, &isAdmin)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, userInfo{
		ID:          sess.UserID,
		Email:       email,
		Username:    username.String,
		DisplayName: display,
		IsAdmin:     isAdmin == 1,
		CSRFToken:   sess.CSRFToken,
	})
}

// ─── middleware ─────────────────────────────────────────────────────────────

// sessionMiddleware loads the session (if any) and attaches it to context.
// Does NOT enforce auth — that's requireAuth's job.
func sessionMiddleware(store *auth.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, err := store.Get(r.Context(), r)
			if err != nil {
				slog.Warn("session load failed", "err", err)
			}
			if sess != nil {
				ctx := context.WithValue(r.Context(), authCtxKey{}, sess)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireAuth rejects requests without a valid session OR with a
// 2FA-pending session.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
		if sess == nil {
			writeErr(w, http.StatusUnauthorized, errors.New("authentication required"))
			return
		}
		if sess.Is2FAPending {
			writeErr(w, http.StatusForbidden, errors.New("2FA required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin rejects requests by non-admin users.
func requireAdmin(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
			if sess == nil {
				writeErr(w, http.StatusUnauthorized, nil)
				return
			}
			var isAdmin int
			if err := db.QueryRowContext(r.Context(),
				`SELECT is_admin FROM users WHERE id = ?`, sess.UserID,
			).Scan(&isAdmin); err != nil || isAdmin != 1 {
				writeErr(w, http.StatusForbidden, errors.New("admin required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// csrfMiddleware enforces X-CSRF-Token header on state-changing methods
// (POST/PUT/PATCH/DELETE). The token must match the session's csrf_token.
// GET/HEAD/OPTIONS are skipped.
func csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
		if sess == nil {
			// requireAuth will catch this; pass through.
			next.ServeHTTP(w, r)
			return
		}

		got := r.Header.Get("X-CSRF-Token")
		if got == "" || got != sess.CSRFToken {
			writeErr(w, http.StatusForbidden, errors.New("invalid or missing CSRF token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── helpers ────────────────────────────────────────────────────────────────

// isSetupComplete returns true if the setup wizard has been completed
// (system_state key = "setup_completed" value = "true").
func isSetupComplete(ctx context.Context, db *sql.DB) (bool, error) {
	var value string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM system_state WHERE key = 'setup_completed'`,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "true", nil
}

// writeErr writes a JSON error response. If err is nil, only status is used.
func writeErr(w http.ResponseWriter, status int, err error) {
	msg := http.StatusText(status)
	if err != nil {
		msg = err.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
