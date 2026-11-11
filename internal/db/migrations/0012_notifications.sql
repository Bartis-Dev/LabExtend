-- +goose Up
-- +goose StatementBegin

-- Threshold alert rules are gone. The leader now emits a fixed set of events
-- (backup failures, node offline/online, container crash loops) that are kept
-- as notifications and sent to every webhook subscribed to their type.
DROP TABLE IF EXISTS alert_history;
DROP TABLE IF EXISTS alert_rules;

CREATE TABLE notifications (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  ts             INTEGER NOT NULL,
  type           TEXT    NOT NULL,
  severity       TEXT    NOT NULL,            -- critical | warning | info
  title          TEXT    NOT NULL,
  body           TEXT    NOT NULL DEFAULT '',
  link           TEXT    NOT NULL DEFAULT '', -- panel path of the related page
  details_json   TEXT    NOT NULL DEFAULT '{}',
  read_at        INTEGER,
  delivery_error TEXT    NOT NULL DEFAULT ''  -- webhooks that failed, '' = all delivered
);
CREATE INDEX idx_notifications_ts ON notifications(ts DESC);

-- JSON array of subscribed event types.
ALTER TABLE webhook_configs ADD COLUMN event_types TEXT NOT NULL DEFAULT '[]';
-- Existing webhooks received backup errors and alerts, so they keep getting
-- every event that replaces them.
UPDATE webhook_configs SET event_types =
  '["backup.failed","backup.retention_failed","node.offline","node.online","container.crashloop"]';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE webhook_configs DROP COLUMN event_types;
DROP TABLE IF EXISTS notifications;
CREATE TABLE alert_rules (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  kind            TEXT NOT NULL,
  comparator      TEXT NOT NULL DEFAULT '>',
  threshold       REAL NOT NULL DEFAULT 0,
  duration_sec    INTEGER NOT NULL DEFAULT 0,
  scope           TEXT NOT NULL DEFAULT 'all',
  webhook_id      TEXT REFERENCES webhook_configs(id) ON DELETE SET NULL,
  cooldown_sec    INTEGER NOT NULL DEFAULT 300,
  enabled         INTEGER NOT NULL DEFAULT 1,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_alert_rules_enabled ON alert_rules(enabled);
CREATE TABLE alert_history (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id      TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
  node_id      TEXT,
  container_id TEXT,
  fired_at     INTEGER NOT NULL,
  state        TEXT NOT NULL,
  value        REAL,
  message      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_alert_history_rule ON alert_history(rule_id, fired_at DESC);
CREATE INDEX idx_alert_history_fired ON alert_history(fired_at DESC);
-- +goose StatementEnd
