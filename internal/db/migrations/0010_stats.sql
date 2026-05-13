-- Stats: push-based time-series metrics.
--
-- Sources are named ingest endpoints with a random token. External
-- scripts POST /api/stats/ingest/{token} with a JSON body containing a
-- numeric value (and optionally a unix-seconds timestamp). Points are
-- stored verbatim — aggregation happens at query time. For a homelab
-- this scales to millions of points before the DB notices.
--
-- Widgets reference a source and render time-series data in the UI.
-- kind is forward-compatible (we ship 'line' first); config_json carries
-- per-kind options like axis label, unit, threshold.

CREATE TABLE IF NOT EXISTS stats_sources (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    unit         TEXT    NOT NULL DEFAULT '',
    token        TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS stats_points (
    source_id INTEGER NOT NULL REFERENCES stats_sources(id) ON DELETE CASCADE,
    ts        INTEGER NOT NULL,
    value     REAL    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_stats_points_source_ts ON stats_points(source_id, ts);

CREATE TABLE IF NOT EXISTS stats_widgets (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id           INTEGER NOT NULL REFERENCES stats_sources(id) ON DELETE CASCADE,
    name                TEXT    NOT NULL,
    kind                TEXT    NOT NULL DEFAULT 'line' CHECK (kind IN ('line','gauge')),
    time_range_minutes  INTEGER NOT NULL DEFAULT 60,
    position            INTEGER NOT NULL DEFAULT 0,
    config_json         TEXT    NOT NULL DEFAULT '{}',
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_stats_widgets_position ON stats_widgets(position);
