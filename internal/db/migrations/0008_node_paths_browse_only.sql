-- +goose Up
-- +goose StatementBegin

-- ── node_paths become browse roots only ────────────────────────────────────
-- The file browser is read-only now; the owner and read-only flags only ever
-- applied to writes.
ALTER TABLE node_paths DROP COLUMN default_uid;
ALTER TABLE node_paths DROP COLUMN default_gid;
ALTER TABLE node_paths DROP COLUMN default_user_label;
ALTER TABLE node_paths DROP COLUMN read_only;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE node_paths ADD COLUMN default_uid INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_paths ADD COLUMN default_gid INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_paths ADD COLUMN default_user_label TEXT;
ALTER TABLE node_paths ADD COLUMN read_only INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
