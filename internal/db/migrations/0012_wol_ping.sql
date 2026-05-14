-- Optional reachability probe per WoL target. The pinger worker tries a
-- TCP connect to ping_host:ping_port every ~10s and surfaces
-- up/down/unknown so the UI can show a status dot. ping_host="" means
-- "not configured" — the UI then shows nothing instead of a red dot.
ALTER TABLE wol_targets ADD COLUMN ping_host TEXT    NOT NULL DEFAULT '';
ALTER TABLE wol_targets ADD COLUMN ping_port INTEGER NOT NULL DEFAULT 22;
