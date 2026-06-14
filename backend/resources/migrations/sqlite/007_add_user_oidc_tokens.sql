-- +goose Up
ALTER TABLE users ADD COLUMN oidc_access_token TEXT;
ALTER TABLE users ADD COLUMN oidc_refresh_token TEXT;
ALTER TABLE users ADD COLUMN oidc_access_token_expires_at DATETIME;

-- +goose Down
ALTER TABLE users DROP COLUMN oidc_access_token;
ALTER TABLE users DROP COLUMN oidc_refresh_token;
ALTER TABLE users DROP COLUMN oidc_access_token_expires_at;
