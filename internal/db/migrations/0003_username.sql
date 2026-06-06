-- +goose Up
-- +goose StatementBegin

-- ── add optional username for login ────────────────────────────────────────
-- Existing users get username=NULL; they keep logging in by email. New users
-- (admin-created or setup-wizard) can opt-in to a username.
ALTER TABLE users ADD COLUMN username TEXT;

-- Partial unique index — NULLs don't collide so existing rows are fine.
CREATE UNIQUE INDEX idx_users_username
  ON users(username)
  WHERE username IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_users_username;
-- (column drop omitted — SQLite supports it from 3.35 but rollback is cheap to skip)
-- +goose StatementEnd
