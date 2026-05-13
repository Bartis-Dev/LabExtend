-- DDNS providers: a named API token for a DNS provider (currently only
-- Cloudflare). Token is AES-GCM-encrypted server-side via servercrypto.
CREATE TABLE IF NOT EXISTS ddns_providers (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 TEXT    NOT NULL,
    kind                 TEXT    NOT NULL CHECK (kind IN ('cloudflare')),
    api_token_ciphertext BLOB    NOT NULL,
    api_token_nonce      BLOB    NOT NULL,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);

-- A "card" pins a remote zone to the dashboard view. It records which
-- record types the user wants to see and where the card sits in the grid.
-- Actual record data is NOT cached here; the frontend queries Cloudflare
-- live (via a backend proxy that injects the token) on render.
CREATE TABLE IF NOT EXISTS ddns_cards (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id  INTEGER NOT NULL REFERENCES ddns_providers(id) ON DELETE CASCADE,
    remote_id    TEXT    NOT NULL,                     -- cloudflare zone id
    name         TEXT    NOT NULL,                     -- zone name, denormalized
    show_types   TEXT    NOT NULL DEFAULT '["A","AAAA"]',
    layout_x     INTEGER NOT NULL DEFAULT 0,
    layout_y     INTEGER NOT NULL DEFAULT 0,
    layout_w     INTEGER NOT NULL DEFAULT 3,
    layout_h     INTEGER NOT NULL DEFAULT 4,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE(provider_id, remote_id)
);

-- A record on auto-update: the worker periodically checks the public IP
-- and updates the record via the provider's API when the IP changed.
CREATE TABLE IF NOT EXISTS ddns_auto_update (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    card_id          INTEGER NOT NULL REFERENCES ddns_cards(id) ON DELETE CASCADE,
    record_remote_id TEXT    NOT NULL,
    record_name      TEXT    NOT NULL,
    record_type      TEXT    NOT NULL CHECK (record_type IN ('A','AAAA')),
    last_synced_ip   TEXT,
    last_synced_at   INTEGER,
    last_error       TEXT,
    created_at       INTEGER NOT NULL,
    UNIQUE(card_id, record_remote_id)
);

CREATE INDEX IF NOT EXISTS idx_ddns_cards_provider ON ddns_cards(provider_id);
CREATE INDEX IF NOT EXISTS idx_ddns_auto_update_card ON ddns_auto_update(card_id);
