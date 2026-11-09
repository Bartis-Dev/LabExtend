package leader

import (
	"encoding/json"
	"errors"
	"log/slog"
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
	enabled, enrolledAt, left := d.TOTP.Status(r.Context(), sess.UserID)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                  enabled,
		"enrolled_at":              enrolledAt,
		"recovery_codes_remaining": left,
	})
}

// reauth re-verifies the caller's password. Enrolling a second factor and
// removing one are both account-takeover primitives once a session is stolen,
// so neither may rest on the session cookie alone.
func (d *AccountDeps) reauth(r *http.Request, userID int64, password string) error {
	if password == "" {
		return errors.New("current password required")
	}
	var hash string
	if err := d.AuthDeps.DB.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return errors.New("current password incorrect")
	}
	ok, err := auth.VerifyPassword(password, hash)
	if err != nil || !ok {
		return errors.New("current password incorrect")
	}
	return nil
}

// passwordBody is the re-authentication payload for the 2FA enrol/disable
// endpoints.
type passwordBody struct {
	Password string `json:"password"`
}

// TOTPBegin creates a fresh secret + QR + recovery codes as a *pending*
// enrollment; the active factor is untouched until TOTPConfirm promotes it.
func (d *AccountDeps) TOTPBegin(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	var body passwordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.reauth(r, sess.UserID, body.Password); err != nil {
		writeErr(w, http.StatusUnauthorized, err)
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
	var body passwordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.reauth(r, sess.UserID, body.Password); err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	if err := d.TOTP.Disable(r.Context(), sess.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "totp.disable", "user", emailString("", sess.UserID), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// TOTPRegenerateRecovery replaces the recovery codes with a fresh set. Like
// enrol/disable it re-authenticates: a stolen session must not be able to mint
// itself a working second factor.
func (d *AccountDeps) TOTPRegenerateRecovery(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	var body passwordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.reauth(r, sess.UserID, body.Password); err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	codes, err := d.TOTP.RegenerateRecoveryCodes(r.Context(), sess.UserID)
	if errors.Is(err, auth.ErrTOTPNotEnabled) {
		writeErr(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "totp.recovery_regenerate", "user", emailString("", sess.UserID), nil)
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
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
	// Changing the password is the one remediation a user has for a stolen
	// cookie, so every other session has to go — otherwise the thief keeps
	// their access for the rest of the session TTL. The caller's own session
	// survives so they are not logged out by their own action.
	if _, err := d.AuthDeps.DB.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE user_id = ? AND id != ?`, sess.UserID, sess.ID); err != nil {
		slog.Error("revoking other sessions after password change failed",
			"user", sess.UserID, "err", err)
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
