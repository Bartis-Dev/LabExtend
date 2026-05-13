-- Trello-style cards on a free canvas. x/y are stored as floats so cards
-- can sit at any subpixel position the user dragged them to. w/h are
-- used for collision detection; the frontend keeps the stored h in sync
-- with the rendered height via a debounced PATCH.
CREATE TABLE IF NOT EXISTS notes_cards (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL DEFAULT '',
    x_real      REAL    NOT NULL DEFAULT 0,
    y_real      REAL    NOT NULL DEFAULT 0,
    w           INTEGER NOT NULL DEFAULT 280,
    h           INTEGER NOT NULL DEFAULT 120,
    color       TEXT    NOT NULL DEFAULT '#475569',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS notes_items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    card_id     INTEGER NOT NULL REFERENCES notes_cards(id) ON DELETE CASCADE,
    text        TEXT    NOT NULL DEFAULT '',
    is_favorite INTEGER NOT NULL DEFAULT 0 CHECK (is_favorite IN (0,1)),
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notes_items_card ON notes_items(card_id, position);
CREATE INDEX IF NOT EXISTS idx_notes_items_favorite ON notes_items(is_favorite) WHERE is_favorite = 1;
