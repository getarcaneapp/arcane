ALTER TABLE IF EXISTS users
  ADD COLUMN IF NOT EXISTS oidc_access_token TEXT,
  ADD COLUMN IF NOT EXISTS oidc_refresh_token TEXT,
  ADD COLUMN IF NOT EXISTS oidc_access_token_expires_at TIMESTAMPTZ;