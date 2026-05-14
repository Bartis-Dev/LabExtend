-- @no-tx
--
-- Two changes in one migration:
--   1. notes_cards / notes_boards get an independent title_color so the
--      title bar background can be themed separately from the border.
--      Empty string means "use border color" — the frontend falls back.
--   2. notes_boards.cols max bumped from 5 to 10. SQLite can't ALTER a
--      CHECK constraint, so we recreate the table. notes_cards has a
--      foreign key to notes_boards, so we first disable foreign keys,
--      do the recreate inside a transaction, then re-enable.
--
-- The @no-tx marker above is read by migrations.go; it skips its own
-- per-file transaction wrapping so PRAGMA foreign_keys (which is a
-- no-op inside transactions) actually takes effect.

PRAGMA foreign_keys = OFF;

BEGIN;

ALTER TABLE notes_cards  ADD COLUMN title_color TEXT NOT NULL DEFAULT '';
ALTER TABLE notes_boards ADD COLUMN title_color TEXT NOT NULL DEFAULT '';

CREATE TABLE notes_boards_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL DEFAULT '',
    x_real      REAL    NOT NULL DEFAULT 0,
    y_real      REAL    NOT NULL DEFAULT 0,
    cols        INTEGER NOT NULL DEFAULT 2 CHECK (cols BETWEEN 2 AND 10),
    color       TEXT    NOT NULL DEFAULT '#475569',
    title_color TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

INSERT INTO notes_boards_new (id, name, x_real, y_real, cols, color, title_color, created_at, updated_at)
SELECT id, name, x_real, y_real, cols, color, title_color, created_at, updated_at
FROM notes_boards;

DROP TABLE notes_boards;
ALTER TABLE notes_boards_new RENAME TO notes_boards;

INSERT INTO schema_migrations(version, applied_at)
VALUES ('0013_notes_colors_and_cols.sql', strftime('%s','now'));

COMMIT;

PRAGMA foreign_keys = ON;
