-- +goose Up
-- Adds pg_dump backup engine support to backup_plans.
--
-- engine = 'tar' (default) — existing volume-tar+gzip→S3 pipeline
-- engine = 'pgdump'        — new: pg_dump -Fc → verify-restore in sidecar → S3
--
-- For pgdump plans, the source list contains DSN-like connection strings:
--   "host=db port=5432 user=supabase_admin dbname=postgres password_secret=<secret-name>"
-- That way the password never lives in the plan row; the agent reads it
-- from a host-mounted Docker secret at /run/secrets/<secret-name>.
--
-- verify_restore controls the sidecar-based integrity check:
--   1 (default for pgdump) = spin up a throwaway postgres container, pg_restore
--     the dump, on failure abort upload + send error event
--   0 = skip verification (faster, less safe)

ALTER TABLE backup_plans ADD COLUMN engine         TEXT    NOT NULL DEFAULT 'tar';
ALTER TABLE backup_plans ADD COLUMN verify_restore INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_plans_engine ON backup_plans(engine);

-- +goose Down
DROP INDEX IF EXISTS idx_plans_engine;
ALTER TABLE backup_plans DROP COLUMN verify_restore;
ALTER TABLE backup_plans DROP COLUMN engine;
