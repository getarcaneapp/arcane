-- +goose Up
ALTER TABLE IF EXISTS users
  ADD COLUMN IF NOT EXISTS oidc_access_token TEXT,
  ADD COLUMN IF NOT EXISTS oidc_refresh_token TEXT,
  ADD COLUMN IF NOT EXISTS oidc_access_token_expires_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE IF EXISTS users
  DROP COLUMN IF EXISTS oidc_access_token,
  DROP COLUMN IF EXISTS oidc_refresh_token,
  DROP COLUMN IF EXISTS oidc_access_token_expires_at;
