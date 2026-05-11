CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS categories (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    border_color TEXT    NOT NULL DEFAULT '#3b82f6',
    layout_x     INTEGER NOT NULL DEFAULT 0,
    layout_y     INTEGER NOT NULL DEFAULT 0,
    layout_w     INTEGER NOT NULL DEFAULT 3,
    layout_h     INTEGER NOT NULL DEFAULT 2,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS services (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT    NOT NULL,
    description         TEXT    NOT NULL DEFAULT '',
    host_primary        TEXT    NOT NULL,
    port_primary        INTEGER,
    host_alt            TEXT,
    port_alt            INTEGER,
    icon_path           TEXT,
    category_id         INTEGER REFERENCES categories(id) ON DELETE SET NULL,
    layout_x            INTEGER NOT NULL DEFAULT 0,
    layout_y            INTEGER NOT NULL DEFAULT 0,
    layout_w            INTEGER NOT NULL DEFAULT 1,
    layout_h            INTEGER NOT NULL DEFAULT 1,
    ping_primary        INTEGER NOT NULL DEFAULT 0,
    ping_alt            INTEGER NOT NULL DEFAULT 0,
    hc_primary_enabled  INTEGER NOT NULL DEFAULT 0,
    hc_primary_url      TEXT,
    hc_alt_enabled      INTEGER NOT NULL DEFAULT 0,
    hc_alt_url          TEXT,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_services_category ON services(category_id);

CREATE TABLE IF NOT EXISTS themes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL UNIQUE,
    palette_json TEXT    NOT NULL,
    custom_css   TEXT    NOT NULL DEFAULT '',
    is_default   INTEGER NOT NULL DEFAULT 0,
    is_active    INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_themes_active ON themes(is_active);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO themes (name, palette_json, custom_css, is_default, is_active, created_at, updated_at)
VALUES (
    'Default Dark',
    '{"--bg":"#0a0a0a","--bg-card":"#141414","--bg-elevated":"#1c1c1c","--fg":"#e5e5e5","--fg-muted":"#9ca3af","--accent":"#6366f1","--accent-hover":"#818cf8","--border":"#262626","--border-strong":"#3f3f46","--danger":"#ef4444","--success":"#22c55e","--warning":"#eab308"}',
    '',
    1,
    1,
    strftime('%s','now'),
    strftime('%s','now')
);
