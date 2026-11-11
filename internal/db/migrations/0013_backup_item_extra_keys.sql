-- +goose Up
-- A pgdump run with several DSNs uploads one object per source. s3_key holds
-- the first; the others are listed here (JSON array) so retention deletes
-- every object of the item, not just one.
ALTER TABLE backup_run_items ADD COLUMN extra_s3_keys TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE backup_run_items DROP COLUMN extra_s3_keys;
