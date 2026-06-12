-- +goose Up
ALTER TABLE users ADD COLUMN oidc_access_token TEXT;
ALTER TABLE users ADD COLUMN oidc_refresh_token TEXT;
ALTER TABLE users ADD COLUMN oidc_access_token_expires_at DATETIME;

-- +goose Down
-- SQLite cannot DROP COLUMN directly. No-op down migration.
-- To rollback manually, recreate the users table without these columns and copy data back.
