-- +goose Up
-- +goose StatementBegin

-- ── Raw per-heartbeat samples for sub-minute UI granularity ───────────────
-- Short retention (default 2h, configurable via BPM_SAMPLE_RETENTION_HOURS).
-- For longer windows the UI queries the already-existing node_metric_buckets
-- (1-min averages, 7-day retention).
--
-- Math: 5s heartbeat × 720 samples/h × 2h × ~100 nodes = 144k rows worst case
-- → ~25 MB. SQLite handles that without breaking a sweat.
CREATE TABLE node_metric_samples (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  node_id         TEXT    NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  ts_ms           INTEGER NOT NULL,
  cpu_percent     REAL    NOT NULL DEFAULT 0,
  mem_pct         REAL    NOT NULL DEFAULT 0,
  disk_pct        REAL    NOT NULL DEFAULT 0,
  net_rx_bps      INTEGER NOT NULL DEFAULT 0,
  net_tx_bps      INTEGER NOT NULL DEFAULT 0,
  disk_read_bps   INTEGER NOT NULL DEFAULT 0,
  disk_write_bps  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_metric_samples_node_ts ON node_metric_samples(node_id, ts_ms);
CREATE INDEX idx_metric_samples_ts      ON node_metric_samples(ts_ms);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS node_metric_samples;
-- +goose StatementEnd
