CREATE TABLE IF NOT EXISTS modules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    slug        TEXT    NOT NULL UNIQUE,
    kind        TEXT    NOT NULL CHECK (kind IN ('builtin','iframe')),
    name        TEXT    NOT NULL,
    icon        TEXT    NOT NULL DEFAULT 'box',
    url         TEXT,
    enabled     INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    position    INTEGER NOT NULL DEFAULT 0,
    builtin_key TEXT UNIQUE,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_modules_enabled_position ON modules(enabled, position);

INSERT OR IGNORE INTO modules (slug, kind, name, icon, url, enabled, position, builtin_key, created_at, updated_at) VALUES
    ('dashboard',   'builtin', 'Dashboard',   'layout-dashboard', NULL, 1, 0, 'dashboard',   strftime('%s','now'), strftime('%s','now')),
    ('ddns',        'builtin', 'DDNS',        'globe',            NULL, 0, 1, 'ddns',        strftime('%s','now'), strftime('%s','now')),
    ('command-lab', 'builtin', 'Command Lab', 'terminal',         NULL, 0, 2, 'command_lab', strftime('%s','now'), strftime('%s','now')),
    ('wol',         'builtin', 'Wake on LAN', 'power',            NULL, 0, 3, 'wol',         strftime('%s','now'), strftime('%s','now')),
    ('secrets',     'builtin', 'Secrets',     'key-round',        NULL, 0, 4, 'secrets',     strftime('%s','now'), strftime('%s','now')),
    ('docs',        'builtin', 'Docs',        'book-open',        NULL, 0, 5, 'docs',        strftime('%s','now'), strftime('%s','now')),
    ('notes',       'builtin', 'Notes',       'notebook-pen',     NULL, 0, 6, 'notes',       strftime('%s','now'), strftime('%s','now')),
    ('stats',       'builtin', 'Stats',       'bar-chart-3',      NULL, 0, 7, 'stats',       strftime('%s','now'), strftime('%s','now'));
