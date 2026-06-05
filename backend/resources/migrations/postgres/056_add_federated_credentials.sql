-- +goose Up
CREATE TABLE IF NOT EXISTS federated_credentials (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    issuer_url TEXT NOT NULL,
    audiences TEXT NOT NULL,
    subject_claim TEXT NOT NULL DEFAULT 'sub',
    subject_match TEXT NOT NULL,
    match_type TEXT NOT NULL DEFAULT 'exact',
    role_id TEXT NOT NULL REFERENCES roles(id),
    environment_id TEXT REFERENCES environments(id) ON DELETE SET NULL,
    identity_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_ttl_seconds INTEGER NOT NULL DEFAULT 900,
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_federated_credentials_issuer_url ON federated_credentials(issuer_url);
CREATE INDEX IF NOT EXISTS idx_federated_credentials_enabled ON federated_credentials(enabled);
CREATE INDEX IF NOT EXISTS idx_federated_credentials_identity_user_id ON federated_credentials(identity_user_id);
CREATE INDEX IF NOT EXISTS idx_federated_credentials_role_id ON federated_credentials(role_id);

CREATE TABLE IF NOT EXISTS federated_token_replays (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    issuer_url TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_federated_token_replays_issuer_url ON federated_token_replays(issuer_url);
CREATE INDEX IF NOT EXISTS idx_federated_token_replays_expires_at ON federated_token_replays(expires_at);

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_service_account BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS source TEXT;
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS federated_credential_id TEXT REFERENCES federated_credentials(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_user_sessions_federated_credential_id ON user_sessions(federated_credential_id);

-- +goose Down
DROP INDEX IF EXISTS idx_user_sessions_federated_credential_id;
ALTER TABLE user_sessions DROP COLUMN IF EXISTS federated_credential_id;
ALTER TABLE user_sessions DROP COLUMN IF EXISTS source;
ALTER TABLE users DROP COLUMN IF EXISTS is_service_account;

DROP INDEX IF EXISTS idx_federated_credentials_role_id;
DROP INDEX IF EXISTS idx_federated_credentials_identity_user_id;
DROP INDEX IF EXISTS idx_federated_credentials_enabled;
DROP INDEX IF EXISTS idx_federated_credentials_issuer_url;
DROP INDEX IF EXISTS idx_federated_token_replays_expires_at;
DROP INDEX IF EXISTS idx_federated_token_replays_issuer_url;
DROP TABLE IF EXISTS federated_token_replays;
DROP TABLE IF EXISTS federated_credentials;
