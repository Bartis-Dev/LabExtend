-- Wake-on-LAN targets. Magic packets are built per request from the MAC,
-- sent to broadcast_addr:port over UDP. The user is responsible for
-- ensuring the container can reach the LAN broadcast — typically via
-- --network host or a routed broadcast.
CREATE TABLE IF NOT EXISTS wol_targets (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL,
    mac            TEXT    NOT NULL,
    broadcast_addr TEXT    NOT NULL DEFAULT '255.255.255.255',
    port           INTEGER NOT NULL DEFAULT 9,
    last_sent_at   INTEGER,
    last_error     TEXT,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
