-- Ensure OIDC subject is unique across users while allowing NULL values
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_subject_id_unique
ON users (oidc_subject_id)
WHERE oidc_subject_id IS NOT NULL;