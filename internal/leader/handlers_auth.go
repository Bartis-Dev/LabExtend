package leader

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
)

// authCtxKey is the request-context key for the *auth.Session attached by
// the session middleware.
type authCtxKey struct{}

// userInfo is the JSON shape returned by /api/me. Besides the profile it
// carries the instance facts the UI needs to shape itself: where the account
// is managed, which groups decide the role, and which features are on.
type userInfo struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	CSRFToken   string `json:"csrf_token"`

	ProviderName string `json:"provider_name"`
	ProviderURL  string `json:"provider_url"`
	UserGroup    string `json:"user_group"`
	AdminGroup   string `json:"admin_group"`
	FileBrowser  bool   `json:"filebrowser"`
	AuditEnabled bool   `json:"audit_enabled"`
}

// AuthDeps groups the pieces the session handlers need.
type AuthDeps struct {
	DB       *sql.DB
	Sessions *auth.SessionStore
	Audit    *AuditLogger

	ProviderName string
	ProviderURL  string
	UserGroup    string
	AdminGroup   string
	FileBrowser  bool
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
		ID:           sess.UserID,
		Email:        email,
		Username:     username.String,
		DisplayName:  display,
		IsAdmin:      isAdmin == 1,
		CSRFToken:    sess.CSRFToken,
		ProviderName: d.ProviderName,
		ProviderURL:  d.ProviderURL,
		UserGroup:    d.UserGroup,
		AdminGroup:   d.AdminGroup,
		FileBrowser:  d.FileBrowser,
		AuditEnabled: d.Audit.State(r.Context()).Enabled,
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

// requireAuth rejects requests without a valid session.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
		if sess == nil {
			writeErr(w, http.StatusUnauthorized, errors.New("authentication required"))
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
			if !isAdminUser(r.Context(), db, sess.UserID) {
				writeErr(w, http.StatusForbidden, errors.New("admin required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isAdminUser reads the role from the users row; a lookup error counts as
// not admin.
func isAdminUser(ctx context.Context, db *sql.DB, userID int64) bool {
	var isAdmin int
	err := db.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin)
	return err == nil && isAdmin == 1
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
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
			writeErr(w, http.StatusForbidden, errors.New("invalid or missing CSRF token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── helpers ────────────────────────────────────────────────────────────────

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
