-- +goose Up
-- +goose StatementBegin

-- totp.Validate accepts one 30s step of skew either side, so without this a
-- code stayed replayable for up to 90 seconds. The highest step a code was
-- accepted for is remembered and only later steps are accepted afterwards.
ALTER TABLE totp_secrets ADD COLUMN last_used_step INTEGER NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE totp_secrets DROP COLUMN last_used_step;
-- +goose StatementEnd
