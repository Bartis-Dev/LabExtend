-- Canvas boards group 2..5 cards in a horizontal row at a single
-- (x, y) on the free canvas. Cards inside a board are addressed by
-- slot_index instead of their own (x, y); they ride along when the
-- board is dragged. Deleting a board cascades to its cards.
CREATE TABLE IF NOT EXISTS notes_boards (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL DEFAULT '',
    x_real      REAL    NOT NULL DEFAULT 0,
    y_real      REAL    NOT NULL DEFAULT 0,
    cols        INTEGER NOT NULL DEFAULT 3 CHECK (cols BETWEEN 2 AND 5),
    color       TEXT    NOT NULL DEFAULT '#475569',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- A card with board_id IS NOT NULL lives inside that board at slot_index.
-- A card with board_id IS NULL is a free-canvas card and uses x_real/y_real.
ALTER TABLE notes_cards ADD COLUMN board_id   INTEGER REFERENCES notes_boards(id) ON DELETE CASCADE;
ALTER TABLE notes_cards ADD COLUMN slot_index INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_notes_cards_board ON notes_cards(board_id, slot_index);
