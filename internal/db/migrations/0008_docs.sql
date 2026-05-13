-- Docs: simple markdown CMS plus external-link bookmarks. A page with
-- is_link=1 stores a link_url and has no markdown body; the frontend
-- renders it as a clickable card that opens the URL in a new tab.
CREATE TABLE IF NOT EXISTS docs_pages (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    slug             TEXT    NOT NULL UNIQUE,
    title            TEXT    NOT NULL,
    category         TEXT    NOT NULL DEFAULT 'General',
    content_markdown TEXT    NOT NULL DEFAULT '',
    is_link          INTEGER NOT NULL DEFAULT 0 CHECK (is_link IN (0,1)),
    link_url         TEXT,
    position         INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_docs_pages_category ON docs_pages(category, position);
