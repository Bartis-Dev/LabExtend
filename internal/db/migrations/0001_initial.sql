-- +goose Up
-- +goose StatementBegin

-- ── system ──────────────────────────────────────────────────────────────────
CREATE TABLE system_state (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- ── users / auth ────────────────────────────────────────────────────────────
CREATE TABLE users (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  email           TEXT NOT NULL UNIQUE,
  display_name    TEXT NOT NULL DEFAULT '',
  password_hash   TEXT NOT NULL,
  is_admin        INTEGER NOT NULL DEFAULT 0,
  is_active       INTEGER NOT NULL DEFAULT 1,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  last_login_at   INTEGER
);
CREATE INDEX idx_users_email ON users(email);

CREATE TABLE totp_secrets (
  user_id        INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  secret_enc     BLOB NOT NULL,
  enabled        INTEGER NOT NULL DEFAULT 0,
  enrolled_at    INTEGER,
  recovery_codes TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE sessions (
  id             TEXT PRIMARY KEY,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at     INTEGER NOT NULL,
  expires_at     INTEGER NOT NULL,
  last_seen      INTEGER NOT NULL,
  ip             TEXT,
  user_agent     TEXT,
  csrf_token     TEXT NOT NULL,
  is_2fa_pending INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- ── nodes ───────────────────────────────────────────────────────────────────
CREATE TABLE nodes (
  id                TEXT PRIMARY KEY,
  hostname          TEXT NOT NULL,
  os                TEXT,
  arch              TEXT,
  version           TEXT,
  labels_json       TEXT NOT NULL DEFAULT '{}',
  capabilities_json TEXT NOT NULL DEFAULT '[]',
  first_seen        INTEGER NOT NULL,
  last_seen         INTEGER NOT NULL,
  status            TEXT NOT NULL DEFAULT 'offline'
);
CREATE INDEX idx_nodes_status ON nodes(status);

CREATE TABLE node_metrics (
  node_id          TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  reported_at      INTEGER NOT NULL,
  uptime_seconds   INTEGER,
  load_avg_1m      REAL,
  mem_used_bytes   INTEGER,
  mem_total_bytes  INTEGER,
  disk_used_bytes  INTEGER,
  disk_total_bytes INTEGER
);

CREATE TABLE node_paths (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  node_id            TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  label              TEXT NOT NULL,
  path               TEXT NOT NULL,
  default_uid        INTEGER NOT NULL,
  default_gid        INTEGER NOT NULL,
  default_user_label TEXT,
  read_only          INTEGER NOT NULL DEFAULT 0,
  created_at         INTEGER NOT NULL,
  UNIQUE(node_id, path)
);
CREATE INDEX idx_node_paths_node ON node_paths(node_id);

-- ── cron ────────────────────────────────────────────────────────────────────
CREATE TABLE cronjobs (
  id          TEXT PRIMARY KEY,
  node_id     TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  schedule    TEXT NOT NULL,
  command     TEXT NOT NULL,
  run_as      TEXT NOT NULL DEFAULT 'root',
  comment     TEXT NOT NULL DEFAULT '',
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_cronjobs_node ON cronjobs(node_id);

-- ── s3 ──────────────────────────────────────────────────────────────────────
CREATE TABLE s3_endpoints (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  endpoint        TEXT NOT NULL,
  region          TEXT NOT NULL DEFAULT 'eu-central',
  access_key      TEXT NOT NULL, -- encrypted with BPM_SECRETS_KEY
  secret_key      TEXT NOT NULL, -- encrypted with BPM_SECRETS_KEY
  path_style      INTEGER NOT NULL DEFAULT 1,
  default_bucket  TEXT,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);

-- ── webhooks ────────────────────────────────────────────────────────────────
CREATE TABLE webhook_configs (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  kind        TEXT NOT NULL DEFAULT 'discord',
  url         TEXT NOT NULL, -- encrypted
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

-- ── backups ─────────────────────────────────────────────────────────────────
CREATE TABLE backup_plans (
  id                TEXT PRIMARY KEY,
  name              TEXT NOT NULL,
  sources_json      TEXT NOT NULL,
  scope_type        TEXT NOT NULL,                -- node|all|label
  scope_value       TEXT,
  s3_endpoint_id    TEXT NOT NULL REFERENCES s3_endpoints(id),
  s3_bucket         TEXT NOT NULL,
  key_template      TEXT NOT NULL,
  schedule          TEXT NOT NULL,
  retention_keep    INTEGER NOT NULL DEFAULT 7,
  compression       TEXT NOT NULL DEFAULT 'gzip', -- gzip|zstd|none
  compression_level INTEGER NOT NULL DEFAULT 6,
  webhook_id        TEXT REFERENCES webhook_configs(id) ON DELETE SET NULL,
  webhook_mode      TEXT NOT NULL DEFAULT 'on-error', -- always|on-error|off
  enabled           INTEGER NOT NULL DEFAULT 1,
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL,
  last_run_at       INTEGER,
  next_run_at       INTEGER
);

CREATE TABLE backup_runs (
  id            TEXT PRIMARY KEY,
  plan_id       TEXT NOT NULL REFERENCES backup_plans(id) ON DELETE CASCADE,
  triggered_by  TEXT NOT NULL,
  started_at    INTEGER NOT NULL,
  finished_at   INTEGER,
  status        TEXT NOT NULL, -- pending|running|success|partial|failed|cancelled
  error_summary TEXT,
  log_excerpt   TEXT
);
CREATE INDEX idx_runs_plan ON backup_runs(plan_id, started_at DESC);

CREATE TABLE backup_run_items (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id         TEXT NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
  node_id        TEXT NOT NULL REFERENCES nodes(id),
  started_at     INTEGER NOT NULL,
  finished_at    INTEGER,
  status         TEXT NOT NULL,
  s3_key         TEXT,
  bytes_uploaded INTEGER,
  file_count     INTEGER,
  sha256         TEXT,
  error          TEXT,
  log_blob       BLOB
);
CREATE INDEX idx_run_items_run ON backup_run_items(run_id);

-- ── audit ───────────────────────────────────────────────────────────────────
CREATE TABLE audit_log (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  ts            INTEGER NOT NULL,
  actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  actor_email   TEXT,
  source_ip     TEXT,
  action        TEXT NOT NULL,
  target_kind   TEXT,
  target_id     TEXT,
  details_json  TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_audit_ts ON audit_log(ts DESC);
CREATE INDEX idx_audit_actor ON audit_log(actor_user_id);
CREATE INDEX idx_audit_action ON audit_log(action);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS backup_run_items;
DROP TABLE IF EXISTS backup_runs;
DROP TABLE IF EXISTS backup_plans;
DROP TABLE IF EXISTS webhook_configs;
DROP TABLE IF EXISTS s3_endpoints;
DROP TABLE IF EXISTS cronjobs;
DROP TABLE IF EXISTS node_paths;
DROP TABLE IF EXISTS node_metrics;
DROP TABLE IF EXISTS nodes;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS totp_secrets;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS system_state;
-- +goose StatementEnd
