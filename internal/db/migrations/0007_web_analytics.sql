-- +goose Up
-- +goose StatementBegin

-- ── Web Analytics: page-view statistics from Traefik access logs ──────────
-- No IP address is stored anywhere. A visitor is counted as
-- sha256(day_salt || ClientHost || 0x00 || User-Agent); the salt and the hashes
-- of a UTC day are deleted once that day is over, so only the per-day COUNT of
-- distinct hashes survives and nobody can be followed across days.

-- One random salt per UTC day, deleted after the day ends.
CREATE TABLE web_visitor_salts (
  day   TEXT PRIMARY KEY,           -- YYYY-MM-DD (UTC)
  salt  BLOB NOT NULL
);

-- Today's visitor hashes. hour = unix seconds of the hour the hash was first
-- seen that day, which gives the hourly "visitors" curve.
CREATE TABLE web_visitor_hashes (
  host  TEXT    NOT NULL,
  day   TEXT    NOT NULL,
  hash  BLOB    NOT NULL,
  hour  INTEGER NOT NULL,
  PRIMARY KEY (host, day, hash)
) WITHOUT ROWID;
CREATE INDEX idx_web_visitor_hashes_hour ON web_visitor_hashes(host, hour);
CREATE INDEX idx_web_visitor_hashes_day  ON web_visitor_hashes(day);

-- Long-term daily totals per host (retention BPM_ANALYTICS_RETENTION_DAYS).
CREATE TABLE web_daily (
  host       TEXT    NOT NULL,
  day        TEXT    NOT NULL,
  requests   INTEGER NOT NULL DEFAULT 0,
  pageviews  INTEGER NOT NULL DEFAULT 0,
  bot_hits   INTEGER NOT NULL DEFAULT 0,
  visitors   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (host, day)
) WITHOUT ROWID;
CREATE INDEX idx_web_daily_day ON web_daily(day);

-- Hourly totals, kept 48h for the 24h chart.
CREATE TABLE web_hourly (
  host       TEXT    NOT NULL,
  hour       INTEGER NOT NULL,      -- unix seconds, start of the hour
  requests   INTEGER NOT NULL DEFAULT 0,
  pageviews  INTEGER NOT NULL DEFAULT 0,
  bot_hits   INTEGER NOT NULL DEFAULT 0,
  visitors   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (host, hour)
) WITHOUT ROWID;
CREATE INDEX idx_web_hourly_hour ON web_hourly(hour);

-- Per-day breakdowns. dim is one of path, referrer, browser, os, status.
-- Capped to the top 50 per (host, day, dim) once the day is over.
CREATE TABLE web_dims (
  host   TEXT    NOT NULL,
  day    TEXT    NOT NULL,
  dim    TEXT    NOT NULL,
  value  TEXT    NOT NULL,
  count  INTEGER NOT NULL DEFAULT 0,
  UNIQUE (host, day, dim, value)
);
CREATE INDEX idx_web_dims_day ON web_dims(day);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS web_dims;
DROP TABLE IF EXISTS web_hourly;
DROP TABLE IF EXISTS web_daily;
DROP TABLE IF EXISTS web_visitor_hashes;
DROP TABLE IF EXISTS web_visitor_salts;
-- +goose StatementEnd
