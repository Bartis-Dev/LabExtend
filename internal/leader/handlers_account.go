package leader

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
)

// AccountDeps groups what /api/account/* (per-user self-service) needs.
type AccountDeps struct {
	AuthDeps *AuthDeps
	TOTP     *auth.TOTPManager
	Audit    *AuditLogger
}

// TOTPStatus reports whether the current user has 2FA enabled.
func (d *AccountDeps) TOTPStatus(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	enabled, enrolledAt := d.TOTP.Status(r.Context(), sess.UserID)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     enabled,
		"enrolled_at": enrolledAt,
	})
}

// TOTPBegin creates a fresh secret + QR + recovery codes (enabled=0).
func (d *AccountDeps) TOTPBegin(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	var email string
	_ = d.AuthDeps.DB.QueryRowContext(r.Context(),
		`SELECT email FROM users WHERE id = ?`, sess.UserID).Scan(&email)
	setup, err := d.TOTP.BeginEnrollment(r.Context(), sess.UserID, email)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "totp.begin", "user", emailString(email, sess.UserID), nil)
	writeJSON(w, http.StatusOK, setup)
}

// TOTPConfirm validates one code against the pending secret and flips enabled=1.
func (d *AccountDeps) TOTPConfirm(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.TOTP.FinishEnrollment(r.Context(), sess.UserID, body.Code); err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	d.Audit.Log(r.Context(), r, "totp.confirm", "user", emailString("", sess.UserID), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// TOTPDisable removes the user's TOTP entirely.
func (d *AccountDeps) TOTPDisable(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	if err := d.TOTP.Disable(r.Context(), sess.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "totp.disable", "user", emailString("", sess.UserID), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ChangePassword updates the user's own password after verifying the current one.
func (d *AccountDeps) ChangePassword(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(body.New) < 12 {
		writeErr(w, http.StatusBadRequest, errors.New("new password must be ≥12 chars"))
		return
	}
	var hash string
	if err := d.AuthDeps.DB.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = ?`, sess.UserID).Scan(&hash); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	ok, _ := auth.VerifyPassword(body.Current, hash)
	if !ok {
		writeErr(w, http.StatusUnauthorized, errors.New("current password incorrect"))
		return
	}
	newHash, err := auth.HashPassword(body.New)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := d.AuthDeps.DB.ExecContext(r.Context(),
		`UPDATE users SET password_hash = ? WHERE id = ?`, newHash, sess.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "user.password_change", "user", emailString("", sess.UserID), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// emailString returns email if non-empty, else "user:<id>".
func emailString(email string, userID int64) string {
	if email != "" {
		return email
	}
	return "user:" + intToA(userID)
}

func intToA(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
