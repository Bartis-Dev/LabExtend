package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/Bartis-Dev/LabExtend/internal/db"
)

func totpTestManager(t *testing.T) (*TOTPManager, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES (1, 'a@example.com', 'x', 0, 0)`); err != nil {
		t.Fatal(err)
	}
	m, err := NewTOTPManager(d, "test", strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	return m, d
}

func enrolled(t *testing.T, m *TOTPManager) *TOTPSetup {
	t.Helper()
	ctx := context.Background()
	setup, err := m.BeginEnrollment(ctx, 1, "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Confirm with the previous window's code so the current one is still fresh
	// for the test that follows.
	code, err := totp.GenerateCode(setup.Secret, time.Now().Add(-30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.FinishEnrollment(ctx, 1, code); err != nil {
		t.Fatal(err)
	}
	return setup
}

// totp.Validate accepts one step of skew either side, so a code stays valid for
// up to 90 seconds. Without a remembered step, a code read over a shoulder or
// out of a proxy log could be replayed for that whole time.
func TestTOTPCodeCannotBeReplayed(t *testing.T) {
	m, _ := totpTestManager(t)
	setup := enrolled(t, m)
	ctx := context.Background()

	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx, 1, code); err != nil {
		t.Fatalf("first use rejected: %v", err)
	}
	if err := m.Verify(ctx, 1, code); err == nil {
		t.Fatal("same code accepted twice")
	}
	// An older window's code must not be usable after a newer one was.
	prev, _ := totp.GenerateCode(setup.Secret, time.Now().Add(-30*time.Second))
	if prev != code {
		if err := m.Verify(ctx, 1, prev); err == nil {
			t.Fatal("older window accepted after a newer one was used")
		}
	}
}

func TestRecoveryCodesRemainingAndRegenerate(t *testing.T) {
	m, _ := totpTestManager(t)
	setup := enrolled(t, m)
	ctx := context.Background()

	if _, _, left := m.Status(ctx, 1); left != 8 {
		t.Fatalf("remaining = %d, want 8", left)
	}
	if err := m.VerifyRecoveryCode(ctx, 1, setup.RecoveryCodes[0]); err != nil {
		t.Fatal(err)
	}
	if _, _, left := m.Status(ctx, 1); left != 7 {
		t.Fatalf("remaining after one use = %d, want 7", left)
	}

	fresh, err := m.RegenerateRecoveryCodes(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) != 8 {
		t.Fatalf("got %d new codes, want 8", len(fresh))
	}
	if _, _, left := m.Status(ctx, 1); left != 8 {
		t.Fatalf("remaining after regenerate = %d, want 8", left)
	}
	if err := m.VerifyRecoveryCode(ctx, 1, setup.RecoveryCodes[1]); err == nil {
		t.Fatal("old recovery code still accepted after regenerate")
	}
	if err := m.VerifyRecoveryCode(ctx, 1, fresh[0]); err != nil {
		t.Fatalf("new recovery code rejected: %v", err)
	}
}

func TestRegenerateRequiresEnabledTOTP(t *testing.T) {
	m, _ := totpTestManager(t)
	if _, err := m.RegenerateRecoveryCodes(context.Background(), 1); err == nil {
		t.Fatal("regenerate succeeded without 2FA enabled")
	}
}
