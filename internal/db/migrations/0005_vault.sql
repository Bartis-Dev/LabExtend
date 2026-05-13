-- Vault state: one row, id=1 always. Stores the KDF parameters and a
-- verifier ciphertext so the frontend can prove it derived the correct
-- key without sending the master password to the backend.
CREATE TABLE IF NOT EXISTS vault_state (
    id                    INTEGER PRIMARY KEY CHECK (id = 1),
    kdf_salt              BLOB    NOT NULL,
    kdf_params_json       TEXT    NOT NULL,
    verifier_ciphertext   BLOB    NOT NULL,
    verifier_nonce        BLOB    NOT NULL,
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
);

-- Entries store one AES-GCM ciphertext per row covering the full payload
-- (name, website, username, secret, notes, totp_secret). Backend never
-- decrypts; everything happens in the browser.
CREATE TABLE IF NOT EXISTS vault_entries (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    payload_ciphertext  BLOB    NOT NULL,
    payload_nonce       BLOB    NOT NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);
