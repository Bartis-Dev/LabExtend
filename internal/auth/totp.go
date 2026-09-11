package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTPSetup is the artifact bundle returned once on enrollment.
type TOTPSetup struct {
	Secret        string   `json:"secret"`
	OTPAuthURL    string   `json:"otpauth_url"`
	QRPNGBase64   string   `json:"qr_png_base64"`
	RecoveryCodes []string `json:"recovery_codes"`
}

// TOTPManager encapsulates AES-GCM-encrypted secret storage and verification.
type TOTPManager struct {
	db       *sql.DB
	issuer   string
	keyBytes []byte // 32 bytes
}

func NewTOTPManager(db *sql.DB, issuer, hexKey string) (*TOTPManager, error) {
	if issuer == "" {
		issuer = "LabExtend"
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("totp key not hex: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("totp key must be 32 bytes (got %d)", len(key))
	}
	return &TOTPManager{db: db, issuer: issuer, keyBytes: key}, nil
}

// BeginEnrollment generates a new secret + 8 recovery codes, stores them
// AES-GCM-encrypted with enabled=0, and returns the artifacts.
func (m *TOTPManager) BeginEnrollment(ctx context.Context, userID int64, accountName string) (*TOTPSetup, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      m.issuer,
		AccountName: accountName,
		Period:      30,
		SecretSize:  20,
	})
	if err != nil {
		return nil, fmt.Errorf("generate: %w", err)
	}

	img, err := key.Image(200, 200)
	if err != nil {
		return nil, fmt.Errorf("qr image: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("png encode: %w", err)
	}
	qrB64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	codes := generateRecoveryCodes(8)
	codesJSON, _ := json.Marshal(toRecoveryEntries(codes))

	encSecret, err := m.encrypt(key.Secret())
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	// Only the pending columns are touched: an enrollment the user starts and
	// abandons must not disturb 2FA that is already working, and neither must
	// one an attacker starts with a stolen session.
	if _, err := m.db.ExecContext(ctx, `
		INSERT INTO totp_secrets (user_id, secret_enc, enabled, recovery_codes,
		                          pending_secret_enc, pending_recovery_codes)
		VALUES (?, X'', 0, '[]', ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			pending_secret_enc     = excluded.pending_secret_enc,
			pending_recovery_codes = excluded.pending_recovery_codes
	`, userID, encSecret, string(codesJSON)); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}

	return &TOTPSetup{
		Secret:        key.Secret(),
		OTPAuthURL:    key.URL(),
		QRPNGBase64:   qrB64,
		RecoveryCodes: codes,
	}, nil
}

// FinishEnrollment verifies one code against the pending secret. On success
// flips enabled=1 and records enrolled_at.
func (m *TOTPManager) FinishEnrollment(ctx context.Context, userID int64, code string) error {
	var enc []byte
	if err := m.db.QueryRowContext(ctx,
		`SELECT pending_secret_enc FROM totp_secrets WHERE user_id = ? AND pending_secret_enc IS NOT NULL`,
		userID).Scan(&enc); err != nil {
		return ErrInvalidCode // no enrollment in progress
	}
	secret, err := m.decrypt(enc)
	if err != nil {
		return err
	}
	if !totp.Validate(code, secret) {
		return ErrInvalidCode
	}
	// Promote the pending enrollment. Guarded on pending_secret_enc so two
	// concurrent confirmations cannot promote the same secret twice.
	_, err = m.db.ExecContext(ctx, `
		UPDATE totp_secrets
		   SET secret_enc             = pending_secret_enc,
		       recovery_codes         = pending_recovery_codes,
		       enabled                = 1,
		       enrolled_at            = ?,
		       pending_secret_enc     = NULL,
		       pending_recovery_codes = NULL
		 WHERE user_id = ? AND pending_secret_enc IS NOT NULL
	`, time.Now().Unix(), userID)
	return err
}

// Verify checks a 6-digit code against the user's stored (decrypted) secret.
// Accepts the current window (no skew — keep it simple, clocks should be NTP-synced).
func (m *TOTPManager) Verify(ctx context.Context, userID int64, code string) error {
	secret, err := m.loadSecret(ctx, userID)
	if err != nil {
		return err
	}
	if totp.Validate(code, secret) {
		return nil
	}
	return ErrInvalidCode
}

// VerifyRecoveryCode consumes one recovery code (marks used=true).
func (m *TOTPManager) VerifyRecoveryCode(ctx context.Context, userID int64, code string) error {
	var codesJSON string
	if err := m.db.QueryRowContext(ctx,
		`SELECT recovery_codes FROM totp_secrets WHERE user_id = ? AND enabled = 1`, userID).
		Scan(&codesJSON); err != nil {
		return ErrInvalidCode
	}
	var entries []recoveryEntry
	if err := json.Unmarshal([]byte(codesJSON), &entries); err != nil {
		return ErrInvalidCode
	}
	want := hashRecoveryCode(code)
	for i, e := range entries {
		if e.Used {
			continue
		}
		var match bool
		switch {
		case e.Hash != "":
			match = subtle.ConstantTimeCompare([]byte(e.Hash), []byte(want)) == 1
		case e.Code != "":
			match = subtle.ConstantTimeCompare([]byte(e.Code), []byte(code)) == 1
		}
		if !match {
			continue
		}
		entries[i].Used = true
		entries[i].UsedAt = time.Now().Unix()
		out, err := json.Marshal(entries)
		if err != nil {
			return err
		}
		// If this write is lost the code stays usable, so a failure here has to
		// deny the login rather than quietly grant a reusable one.
		if _, err := m.db.ExecContext(ctx,
			`UPDATE totp_secrets SET recovery_codes = ? WHERE user_id = ?`, string(out), userID); err != nil {
			return err
		}
		return nil
	}
	return ErrInvalidCode
}

// Disable removes the user's TOTP entry entirely.
func (m *TOTPManager) Disable(ctx context.Context, userID int64) error {
	_, err := m.db.ExecContext(ctx, `DELETE FROM totp_secrets WHERE user_id = ?`, userID)
	return err
}

// Status reports whether the user has 2FA enabled.
func (m *TOTPManager) Status(ctx context.Context, userID int64) (enabled bool, enrolledAt int64) {
	var en int
	var ea sql.NullInt64
	_ = m.db.QueryRowContext(ctx,
		`SELECT enabled, enrolled_at FROM totp_secrets WHERE user_id = ?`, userID,
	).Scan(&en, &ea)
	if ea.Valid {
		enrolledAt = ea.Int64
	}
	return en == 1, enrolledAt
}

// loadSecret decrypts the stored secret.
func (m *TOTPManager) loadSecret(ctx context.Context, userID int64) (string, error) {
	var enc []byte
	if err := m.db.QueryRowContext(ctx,
		`SELECT secret_enc FROM totp_secrets WHERE user_id = ?`, userID).Scan(&enc); err != nil {
		return "", ErrInvalidCode
	}
	return m.decrypt(enc)
}

// ─── AES-GCM helpers ────────────────────────────────────────────────────────

func (m *TOTPManager) encrypt(plaintext string) ([]byte, error) {
	gcm, err := m.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return append(nonce, ct...), nil
}

func (m *TOTPManager) decrypt(raw []byte) (string, error) {
	gcm, err := m.gcm()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func (m *TOTPManager) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(m.keyBytes)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// ─── recovery codes ─────────────────────────────────────────────────────────

type recoveryEntry struct {
	// Hash is sha256(code) in hex. The codes used to be stored in cleartext
	// right next to the AES-GCM-encrypted secret, which handed anyone with a
	// copy of the database eight working second factors per user and defeated
	// the point of encrypting the secret at all. Plain sha256 is the right
	// primitive here rather than a slow KDF: these are 50-bit random codes,
	// not passwords.
	Hash string `json:"hash,omitempty"`
	// Code is the legacy cleartext form, still accepted so recovery sheets
	// printed before the change keep working until the user re-enrolls.
	Code   string `json:"code,omitempty"`
	Used   bool   `json:"used"`
	UsedAt int64  `json:"used_at,omitempty"`
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func toRecoveryEntries(codes []string) []recoveryEntry {
	out := make([]recoveryEntry, len(codes))
	for i, c := range codes {
		out[i] = recoveryEntry{Hash: hashRecoveryCode(c)}
	}
	return out
}

// generateRecoveryCodes returns n base32 codes (10 chars each).
func generateRecoveryCodes(n int) []string {
	out := make([]string, n)
	for i := range out {
		raw := make([]byte, 7) // 7 bytes → 56 bits → 12 base32 chars
		_, _ = rand.Read(raw)
		s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
		out[i] = s[:10]
	}
	return out
}

// ErrInvalidCode signals an authentication failure on TOTP / recovery code.
var ErrInvalidCode = errors.New("invalid 2fa code")

// _ = otp.AlgorithmSHA1 ensures the otp import stays alive even if unused.
var _ = otp.AlgorithmSHA1
