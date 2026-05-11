package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
)

const sessionCookieName = "labextend_session"

type setupReq struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleSetup creates the first user. It is a no-op once any user exists,
// so it cannot be used to take over an existing installation.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !s.SetupLimit.Allow("global") {
		writeError(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	var req setupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.Password != req.PasswordConfirm {
		writeError(w, http.StatusBadRequest, "passwords do not match")
		return
	}

	var existing int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&existing); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if existing > 0 {
		writeError(w, http.StatusConflict, "already set up")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash error")
		return
	}
	res, err := s.DB.Exec(
		`INSERT INTO users(username, password_hash, created_at) VALUES (?, ?, ?)`,
		req.Username, hash, time.Now().Unix(),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	id, _ := res.LastInsertId()
	s.issueSession(w, r, id, req.Username)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

// handleLogin verifies credentials and sets the session cookie.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.LoginLimit.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts")
		return
	}
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	var (
		id   int64
		hash string
	)
	err := s.DB.QueryRow(
		`SELECT id, password_hash FROM users WHERE username = ?`, req.Username,
	).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, hash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.issueSession(w, r, id, req.Username)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isSecure(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	c := userFromCtx(r)
	if c == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": c.Username})
}

type changePasswordReq struct {
	Current        string `json:"current"`
	New            string `json:"new"`
	NewConfirm     string `json:"new_confirm"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	c := userFromCtx(r)
	if c == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req changePasswordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.New) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	if req.New != req.NewConfirm {
		writeError(w, http.StatusBadRequest, "passwords do not match")
		return
	}
	var hash string
	if err := s.DB.QueryRow(`SELECT password_hash FROM users WHERE id=?`, c.UserID).Scan(&hash); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	ok, err := auth.VerifyPassword(req.Current, hash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "current password incorrect")
		return
	}
	newHash, err := auth.HashPassword(req.New)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash error")
		return
	}
	if _, err := s.DB.Exec(`UPDATE users SET password_hash=? WHERE id=?`, newHash, c.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// issueSession signs a JWT and sets it as an HTTP-only cookie.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID int64, username string) {
	tok, err := auth.Issue(s.JWTSecret, userID, username, s.Cfg.SessionTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token error")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    tok,
		Path:     "/",
		Expires:  time.Now().Add(s.Cfg.SessionTimeout),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isSecure(r),
	})
}

func isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
