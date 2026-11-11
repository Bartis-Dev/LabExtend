-- +goose Up
-- +goose StatementBegin

-- Pocket ID is the only login now: passwords, TOTP, the setup wizard and local
-- user management are gone. Users are provisioned and their role refreshed from
-- Pocket ID group membership on every login. The rows themselves stay, so the
-- existing linked account keeps its id, sessions and audit history.
DROP TABLE IF EXISTS totp_secrets;
ALTER TABLE users DROP COLUMN password_hash;
-- Group membership is the only on/off switch now; a flag nobody can set would
-- just be a second, invisible one.
ALTER TABLE users DROP COLUMN is_active;
ALTER TABLE sessions DROP COLUMN is_2fa_pending;
DELETE FROM system_state WHERE key = 'setup_completed';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN is_2fa_pending INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN is_active INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
CREATE TABLE totp_secrets (
  user_id                INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  secret_enc             BLOB NOT NULL,
  enabled                INTEGER NOT NULL DEFAULT 0,
  enrolled_at            INTEGER,
  recovery_codes         TEXT NOT NULL DEFAULT '[]',
  pending_secret_enc     BLOB,
  pending_recovery_codes TEXT,
  last_used_step         INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO system_state (key, value) VALUES ('setup_completed', 'true');
-- +goose StatementEnd
