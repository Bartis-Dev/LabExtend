-- +goose Up
-- +goose StatementBegin

-- Explicit OIDC account link. A user is matched by (issuer, subject), never by
-- email: an email claim is whatever the provider says it is, and matching on it
-- would let anyone who controls an address at the provider take over the
-- LabExtend account that uses it. The issuer is part of the key so a later
-- issuer change cannot map an old subject onto someone else.
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;
ALTER TABLE users ADD COLUMN oidc_subject TEXT;

CREATE UNIQUE INDEX idx_users_oidc
  ON users(oidc_issuer, oidc_subject)
  WHERE oidc_subject IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_users_oidc;
ALTER TABLE users DROP COLUMN oidc_subject;
ALTER TABLE users DROP COLUMN oidc_issuer;
-- +goose StatementEnd
