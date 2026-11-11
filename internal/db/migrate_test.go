package db

import (
	"context"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// 0011 runs against the production shape: one linked admin that still has a
// password hash, TOTP and a session from the old login.
func TestMigration0011KeepsLinkedUser(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, d, "migrations", 10); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, username, display_name, password_hash, is_admin, is_active, created_at, updated_at, oidc_issuer, oidc_subject)
		 VALUES (1, 'b@example.com', 'bartis', 'Bartis', '$argon2id$x', 1, 1, 0, 0, 'https://id.example', 'sub-1')`,
		`INSERT INTO totp_secrets (user_id, secret_enc, enabled) VALUES (1, X'00', 1)`,
		`INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen, csrf_token, is_2fa_pending) VALUES ('s', 1, 0, 9999999999, 0, 'c', 0)`,
		`INSERT INTO audit_log (ts, actor_user_id, action) VALUES (0, 1, 'x')`,
		`INSERT INTO system_state (key, value) VALUES ('setup_completed', 'true')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	if err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}

	var email, sub string
	var admin int
	if err := d.QueryRow(`SELECT email, oidc_subject, is_admin FROM users WHERE id = 1`).Scan(&email, &sub, &admin); err != nil {
		t.Fatal(err)
	}
	if email != "b@example.com" || sub != "sub-1" || admin != 1 {
		t.Fatalf("user changed: %s %s %d", email, sub, admin)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = 1`).Scan(&n)
	if n != 1 {
		t.Fatal("session lost")
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&n)
	if n != 1 {
		t.Fatal("audit history lost")
	}
	for _, q := range []string{
		`SELECT password_hash FROM users`,
		`SELECT is_active FROM users`,
		`SELECT is_2fa_pending FROM sessions`,
		`SELECT 1 FROM totp_secrets`,
	} {
		if _, err := d.Exec(q); err == nil {
			t.Errorf("still present after 0011: %s", q)
		}
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM system_state WHERE key = 'setup_completed'`).Scan(&n)
	if n != 0 {
		t.Fatal("setup_completed left behind")
	}
}

// 0012 replaces alert rules with event notifications. The production webhook
// received backup errors and alerts before, so it must keep receiving them.
func TestMigration0012SubscribesExistingWebhooks(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, d, "migrations", 11); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO webhook_configs (id, name, kind, url, enabled, created_at, updated_at)
		 VALUES ('wh', 'Discord', 'discord', 'https://discord.example/x', 1, 0, 0)`,
		`INSERT INTO alert_rules (id, name, kind, webhook_id, created_at, updated_at)
		 VALUES ('r', 'offline', 'node_offline', 'wh', 0, 0)`,
		`INSERT INTO alert_history (rule_id, fired_at, state) VALUES ('r', 0, 'triggered')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	if err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}

	var types string
	if err := d.QueryRow(`SELECT event_types FROM webhook_configs WHERE id = 'wh'`).Scan(&types); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"backup.failed", "node.offline", "node.online", "container.crashloop"} {
		if !strings.Contains(types, `"`+want+`"`) {
			t.Errorf("existing webhook not subscribed to %s: %s", want, types)
		}
	}
	for _, q := range []string{`SELECT 1 FROM alert_rules`, `SELECT 1 FROM alert_history`} {
		if _, err := d.Exec(q); err == nil {
			t.Errorf("still present after 0012: %s", q)
		}
	}
	if _, err := d.Exec(`INSERT INTO notifications (ts, type, severity, title) VALUES (1, 'node.offline', 'critical', 't')`); err != nil {
		t.Fatalf("notifications table: %v", err)
	}

	if err := goose.DownToContext(ctx, d, "migrations", 11); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := d.Exec(`SELECT 1 FROM alert_rules`); err != nil {
		t.Fatalf("down did not restore alert_rules: %v", err)
	}
}
