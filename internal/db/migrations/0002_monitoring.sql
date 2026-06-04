-- +goose Up
-- +goose StatementBegin

-- ── extend node_metrics with the new fields the agent now ships ────────────
ALTER TABLE node_metrics ADD COLUMN cpu_percent       REAL    NOT NULL DEFAULT 0;
ALTER TABLE node_metrics ADD COLUMN cpu_cores         INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_metrics ADD COLUMN net_rx_bytes      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_metrics ADD COLUMN net_tx_bytes      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_metrics ADD COLUMN disk_read_bytes   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_metrics ADD COLUMN disk_write_bytes  INTEGER NOT NULL DEFAULT 0;
-- Computed per-sample (against previous sample) — leader stores rate-of-change
-- so the UI doesn't have to.
ALTER TABLE node_metrics ADD COLUMN net_rx_bps        INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_metrics ADD COLUMN net_tx_bps        INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_metrics ADD COLUMN disk_read_bps     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_metrics ADD COLUMN disk_write_bps    INTEGER NOT NULL DEFAULT 0;

-- ── minute-bucket downsampled history for graphs / averages ────────────────
-- Retention pruned periodically (24h default). One row per node per minute.
CREATE TABLE node_metric_buckets (
  node_id          TEXT    NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  bucket_minute    INTEGER NOT NULL,  -- unix-ts truncated to minute
  cpu_percent      REAL    NOT NULL DEFAULT 0,
  mem_pct          REAL    NOT NULL DEFAULT 0,
  disk_pct         REAL    NOT NULL DEFAULT 0,
  net_rx_bps       INTEGER NOT NULL DEFAULT 0,
  net_tx_bps       INTEGER NOT NULL DEFAULT 0,
  disk_read_bps    INTEGER NOT NULL DEFAULT 0,
  disk_write_bps   INTEGER NOT NULL DEFAULT 0,
  sample_count     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, bucket_minute)
);
CREATE INDEX idx_metric_buckets_ts ON node_metric_buckets(bucket_minute);

-- ── alert rules ────────────────────────────────────────────────────────────
-- Examples:
--   {kind:"cpu_percent",  op:">",  value:80,  duration_sec:60, scope:"all"}
--   {kind:"mem_percent",  op:">",  value:90,  duration_sec:120}
--   {kind:"disk_percent", op:">",  value:85}
--   {kind:"disk_free_gb", op:"<",  value:5,   scope:"node:bd-manager"}
--   {kind:"node_offline", op:">",  value:60,  duration_sec:60}
--   {kind:"container_crashed", scope:"all"} — triggers when any container
--                                            enters crashed_loop state
CREATE TABLE alert_rules (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  kind            TEXT NOT NULL,
  comparator      TEXT NOT NULL DEFAULT '>',  -- > | >= | < | <=
  threshold       REAL NOT NULL DEFAULT 0,
  duration_sec    INTEGER NOT NULL DEFAULT 0, -- 0 = fire immediately
  scope           TEXT NOT NULL DEFAULT 'all',-- all | node:<hostname> | label:<k=v>
  webhook_id      TEXT REFERENCES webhook_configs(id) ON DELETE SET NULL,
  cooldown_sec    INTEGER NOT NULL DEFAULT 300, -- min gap between repeated fires
  enabled         INTEGER NOT NULL DEFAULT 1,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_alert_rules_enabled ON alert_rules(enabled);

-- One row per alert state-change (trigger or recover).
CREATE TABLE alert_history (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id      TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
  node_id      TEXT,                  -- nullable for cluster-wide rules
  container_id TEXT,                  -- nullable for non-container rules
  fired_at     INTEGER NOT NULL,
  state        TEXT NOT NULL,         -- 'triggered' | 'recovered'
  value        REAL,                  -- the metric value that caused the change
  message      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_alert_history_rule ON alert_history(rule_id, fired_at DESC);
CREATE INDEX idx_alert_history_fired ON alert_history(fired_at DESC);

-- ── container state (current snapshot per (node, container)) ────────────────
-- Updated every ~5s from agent ContainerReport events.
CREATE TABLE container_state (
  node_id           TEXT    NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  container_id      TEXT    NOT NULL,
  name              TEXT    NOT NULL DEFAULT '',
  image             TEXT    NOT NULL DEFAULT '',
  state             TEXT    NOT NULL DEFAULT '',
  health            TEXT    NOT NULL DEFAULT '',
  started_at_ms     INTEGER NOT NULL DEFAULT 0,
  finished_at_ms    INTEGER NOT NULL DEFAULT 0,
  restart_count     INTEGER NOT NULL DEFAULT 0,
  recent_restarts   INTEGER NOT NULL DEFAULT 0,
  crashed_loop      INTEGER NOT NULL DEFAULT 0,
  exit_code         INTEGER NOT NULL DEFAULT 0,
  cpu_percent       REAL    NOT NULL DEFAULT 0,
  mem_used_bytes    INTEGER NOT NULL DEFAULT 0,
  mem_limit_bytes   INTEGER NOT NULL DEFAULT 0,
  net_rx_bytes      INTEGER NOT NULL DEFAULT 0,
  net_tx_bytes      INTEGER NOT NULL DEFAULT 0,
  net_rx_bps        INTEGER NOT NULL DEFAULT 0,
  net_tx_bps        INTEGER NOT NULL DEFAULT 0,
  block_read_bytes  INTEGER NOT NULL DEFAULT 0,
  block_write_bytes INTEGER NOT NULL DEFAULT 0,
  labels_json       TEXT    NOT NULL DEFAULT '{}',
  reported_at       INTEGER NOT NULL,
  PRIMARY KEY (node_id, container_id)
);
CREATE INDEX idx_container_state_state    ON container_state(state);
CREATE INDEX idx_container_state_crashed  ON container_state(crashed_loop) WHERE crashed_loop = 1;

-- ── container log ringbuffer ────────────────────────────────────────────────
-- Agent always ships logs for every container it sees. Leader persists into
-- this table with a per-container cap enforced by background prune. Default
-- cap: 5000 lines per container — overridable via BPM_LOG_MAX_LINES.
CREATE TABLE container_log_lines (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  node_id       TEXT    NOT NULL,
  container_id  TEXT    NOT NULL,
  ts_ms         INTEGER NOT NULL,
  stream        TEXT    NOT NULL DEFAULT 'stdout',
  line          TEXT    NOT NULL
);
CREATE INDEX idx_logs_container_id ON container_log_lines(node_id, container_id, id);
CREATE INDEX idx_logs_ts            ON container_log_lines(ts_ms);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS container_log_lines;
DROP TABLE IF EXISTS container_state;
DROP TABLE IF EXISTS alert_history;
DROP TABLE IF EXISTS alert_rules;
DROP TABLE IF EXISTS node_metric_buckets;
-- (column drops are deliberately omitted — SQLite ALTER TABLE DROP COLUMN
-- exists since 3.35 but rollback is cheap to skip.)
-- +goose StatementEnd
