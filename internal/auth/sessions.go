package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"time"
)

// Session represents a persisted browser session row from the `sessions`
// table. Cookie value is the .ID (opaque random hex).
type Session struct {
	ID           string
	UserID       int64
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastSeen     time.Time
	IP           string
	UserAgent    string
	CSRFToken    string
	Is2FAPending bool
}

// SessionStore persists sessions in SQLite. Single backing table = simple
// revocation: DELETE FROM sessions WHERE user_id = ?.
type SessionStore struct {
	db         *sql.DB
	cookieName string
	ttl        time.Duration
	secure     bool
}

// NewSessionStore wires a SessionStore to a SQL connection.
func NewSessionStore(db *sql.DB, cookieName string, ttl time.Duration, secure bool) *SessionStore {
	return &SessionStore{db: db, cookieName: cookieName, ttl: ttl, secure: secure}
}

// Create persists a new session for the given user, sets the cookie on w,
// and returns the freshly-minted session.
func (s *SessionStore) Create(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	userID int64,
	pending2FA bool,
) (*Session, error) {
	id, err := NewSessionID()
	if err != nil {
		return nil, err
	}
	csrf, err := NewCSRFToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	sess := &Session{
		ID:           id,
		UserID:       userID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(s.ttl),
		LastSeen:     now,
		IP:           clientIP(r),
		UserAgent:    truncate(r.Header.Get("User-Agent"), 255),
		CSRFToken:    csrf,
		Is2FAPending: pending2FA,
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions
			(id, user_id, created_at, expires_at, last_seen,
			 ip, user_agent, csrf_token, is_2fa_pending)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		sess.ID, sess.UserID,
		sess.CreatedAt.Unix(), sess.ExpiresAt.Unix(), sess.LastSeen.Unix(),
		sess.IP, sess.UserAgent, sess.CSRFToken,
		boolToInt(sess.Is2FAPending),
	)
	if err != nil {
		return nil, err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    sess.ID,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})

	return sess, nil
}

// Get loads the session indicated by the request's cookie. Returns
// (nil, nil) if no cookie is present or the session is expired/missing.
func (s *SessionStore) Get(ctx context.Context, r *http.Request) (*Session, error) {
	c, err := r.Cookie(s.cookieName)
	if err != nil {
		return nil, nil // no cookie = no session (not an error)
	}
	if c.Value == "" {
		return nil, nil
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, created_at, expires_at, last_seen,
		       COALESCE(ip,''), COALESCE(user_agent,''), csrf_token, is_2fa_pending
		FROM sessions WHERE id = ?
	`, c.Value)

	var (
		sess                                 Session
		createdAt, expiresAt, lastSeen       int64
		pending2FA                           int
	)
	if err := row.Scan(
		&sess.ID, &sess.UserID, &createdAt, &expiresAt, &lastSeen,
		&sess.IP, &sess.UserAgent, &sess.CSRFToken, &pending2FA,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	sess.CreatedAt = time.Unix(createdAt, 0)
	sess.ExpiresAt = time.Unix(expiresAt, 0)
	sess.LastSeen = time.Unix(lastSeen, 0)
	sess.Is2FAPending = pending2FA == 1

	if time.Now().After(sess.ExpiresAt) {
		// Expired — clean up + report no-session.
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sess.ID)
		return nil, nil
	}

	// Touch last_seen (best effort, ignore error)
	now := time.Now().Unix()
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen = ? WHERE id = ?`, now, sess.ID)

	return &sess, nil
}

// Destroy revokes the current session (DELETE row + clear cookie).
func (s *SessionStore) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(s.cookieName)
	if err == nil && c.Value != "" {
		if _, derr := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, c.Value); derr != nil {
			return derr
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// PromoteFrom2FA flips is_2fa_pending=0 after a successful TOTP/recovery
// check. Cookie value stays the same.
func (s *SessionStore) PromoteFrom2FA(ctx context.Context, sess *Session) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET is_2fa_pending = 0 WHERE id = ?`, sess.ID)
	if err == nil {
		sess.Is2FAPending = false
	}
	return err
}

// PurgeExpired deletes rows past their expires_at. Call from a goroutine
// every few minutes.
func (s *SessionStore) PurgeExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
	return err
}

// NewSessionID returns a 32-byte hex token suitable for use as the session
// cookie value.
func NewSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewCSRFToken returns a 16-byte hex token attached to a session and
// required as the X-CSRF-Token header on state-changing requests.
func NewCSRFToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list (the original client).
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	if r.RemoteAddr == "" {
		return ""
	}
	// Strip port.
	for i := len(r.RemoteAddr) - 1; i >= 0; i-- {
		if r.RemoteAddr[i] == ':' {
			return r.RemoteAddr[:i]
		}
	}
	return r.RemoteAddr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
