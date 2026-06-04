package auth

import (
	"context"
	"errors"
)

// TOTPSetup represents the artifacts of a 2FA enrollment that the UI must
// show to the user once (QR + secret + recovery codes).
type TOTPSetup struct {
	Secret        string   // base32 secret (display in QR)
	OTPAuthURL    string   // otpauth://totp/Issuer:account?secret=...&issuer=...
	QRPNGBase64   string   // pre-rendered PNG (base64), for inline <img src="data:image/png;base64,...">
	RecoveryCodes []string // 8 one-time codes; shown ONCE, never again
}

// TOTPManager encapsulates AES-GCM-encrypted secret storage and verification.
// The encryption key is BPM_TOTP_KEY (32-byte hex).
type TOTPManager struct {
	issuer   string
	keyBytes []byte // 32 bytes, decoded from hex once at boot
}

// NewTOTPManager parses the hex-encoded key and returns a manager.
// TODO(phase 10): validate key length == 32 bytes; decode strictly.
func NewTOTPManager(issuer, hexKey string) (*TOTPManager, error) {
	_, _ = issuer, hexKey
	return nil, errors.New("NewTOTPManager: TODO(phase 10)")
}

// BeginEnrollment generates a new TOTP secret for the user, persists it as
// disabled (so the user must verify a code to enable it), and returns the
// artifacts the UI needs.
// TODO(phase 10): use pquerna/otp/totp.Generate(); render QR via
// otp.Key.Image(200,200); encrypt secret with AES-GCM; INSERT totp_secrets.
func (m *TOTPManager) BeginEnrollment(_ context.Context, _ int64, _ string) (*TOTPSetup, error) {
	return nil, errors.New("BeginEnrollment: TODO(phase 10)")
}

// FinishEnrollment verifies one code against the pending secret; on success
// flips totp_secrets.enabled = 1 and returns the recovery codes.
// TODO(phase 10).
func (m *TOTPManager) FinishEnrollment(_ context.Context, _ int64, _ string) ([]string, error) {
	return nil, errors.New("FinishEnrollment: TODO(phase 10)")
}

// Verify checks a 6-digit code against the user's stored (decrypted) secret.
// Accepts ±1 step skew. Returns ErrInvalidCode on mismatch.
// TODO(phase 10).
func (m *TOTPManager) Verify(_ context.Context, _ int64, _ string) error {
	return errors.New("Verify: TODO(phase 10)")
}

// VerifyRecoveryCode consumes one recovery code. Marks it as used in the JSON
// array stored in totp_secrets.recovery_codes.
// TODO(phase 10).
func (m *TOTPManager) VerifyRecoveryCode(_ context.Context, _ int64, _ string) error {
	return errors.New("VerifyRecoveryCode: TODO(phase 10)")
}

// Disable removes the TOTP secret entirely (user-initiated, with password
// re-confirmation enforced by the caller).
// TODO(phase 10).
func (m *TOTPManager) Disable(_ context.Context, _ int64) error {
	return errors.New("Disable: TODO(phase 10)")
}

// ErrInvalidCode signals an authentication failure on TOTP / recovery code.
var ErrInvalidCode = errors.New("invalid 2fa code")
