ALTER TABLE IF EXISTS users
  DROP COLUMN IF EXISTS oidc_access_token,
  DROP COLUMN IF EXISTS oidc_refresh_token,
  DROP COLUMN IF EXISTS oidc_access_token_expires_at;