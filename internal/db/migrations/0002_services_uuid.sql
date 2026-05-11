-- Add uuid column to services so the public identifier is a string
-- (allows duplicate names freely, decouples API URLs from row IDs).
ALTER TABLE services ADD COLUMN uuid TEXT;

-- Backfill any existing rows with a random 32-char hex UUID.
UPDATE services
SET uuid = lower(hex(randomblob(16)))
WHERE uuid IS NULL OR uuid = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_services_uuid ON services(uuid);
