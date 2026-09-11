-- +goose Up
-- +goose StatementBegin

-- Enrollment used to write the new secret straight into secret_enc with
-- enabled=0, so merely opening the "set up 2FA" panel disabled working 2FA and
-- destroyed the recovery codes with it — no password, no confirmation, no
-- notice, and every later login silently skipped the second factor.
--
-- A pending enrollment now lives in its own columns and is promoted only when
-- a valid code confirms it.
ALTER TABLE totp_secrets ADD COLUMN pending_secret_enc BLOB;
ALTER TABLE totp_secrets ADD COLUMN pending_recovery_codes TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE totp_secrets DROP COLUMN pending_recovery_codes;
ALTER TABLE totp_secrets DROP COLUMN pending_secret_enc;
-- +goose StatementEnd
