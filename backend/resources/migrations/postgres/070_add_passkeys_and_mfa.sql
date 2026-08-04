-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS passkey_mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS mfa_method TEXT;
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS mfa_verified_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS passkeys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rp_id TEXT NOT NULL,
    credential_id BYTEA NOT NULL,
    public_key BYTEA NOT NULL,
    attestation_type TEXT,
    attestation_format TEXT,
    transports TEXT,
    aaguid BYTEA,
    sign_count INTEGER NOT NULL DEFAULT 0,
    backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state BOOLEAN NOT NULL DEFAULT FALSE,
    clone_warning BOOLEAN NOT NULL DEFAULT FALSE,
    authenticator_attachment TEXT,
    attestation_client_data_json BYTEA,
    attestation_client_data_hash BYTEA,
    attestation_authenticator_data BYTEA,
    attestation_public_key_algorithm BIGINT,
    attestation_object BYTEA,
    name TEXT NOT NULL,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_passkeys_rp_credential ON passkeys(rp_id, credential_id);
CREATE INDEX IF NOT EXISTS idx_passkeys_user_id ON passkeys(user_id);

CREATE TABLE IF NOT EXISTS auth_transactions (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id TEXT,
    source TEXT NOT NULL,
    user_agent TEXT,
    ip_address TEXT,
    secret_hash TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_auth_transactions_user_id ON auth_transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_transactions_status ON auth_transactions(status);
CREATE INDEX IF NOT EXISTS idx_auth_transactions_secret_hash ON auth_transactions(secret_hash);
CREATE INDEX IF NOT EXISTS idx_auth_transactions_expires_at ON auth_transactions(expires_at);

CREATE TABLE IF NOT EXISTS passkey_ceremonies (
    id TEXT PRIMARY KEY,
    purpose TEXT NOT NULL,
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    session_id TEXT,
    auth_transaction_id TEXT REFERENCES auth_transactions(id) ON DELETE CASCADE,
    rp_id TEXT NOT NULL,
    session_data TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_passkey_ceremonies_user_id ON passkey_ceremonies(user_id);
CREATE INDEX IF NOT EXISTS idx_passkey_ceremonies_transaction_id ON passkey_ceremonies(auth_transaction_id);
CREATE INDEX IF NOT EXISTS idx_passkey_ceremonies_expires_at ON passkey_ceremonies(expires_at);

CREATE TABLE IF NOT EXISTS passkey_recovery_codes (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL UNIQUE,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_passkey_recovery_codes_user_id ON passkey_recovery_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_passkey_recovery_codes_used_at ON passkey_recovery_codes(used_at);

-- +goose Down
DROP INDEX IF EXISTS idx_passkey_recovery_codes_used_at;
DROP INDEX IF EXISTS idx_passkey_recovery_codes_user_id;
DROP TABLE IF EXISTS passkey_recovery_codes;

DROP INDEX IF EXISTS idx_passkey_ceremonies_expires_at;
DROP INDEX IF EXISTS idx_passkey_ceremonies_transaction_id;
DROP INDEX IF EXISTS idx_passkey_ceremonies_user_id;
DROP TABLE IF EXISTS passkey_ceremonies;

DROP INDEX IF EXISTS idx_auth_transactions_expires_at;
DROP INDEX IF EXISTS idx_auth_transactions_secret_hash;
DROP INDEX IF EXISTS idx_auth_transactions_status;
DROP INDEX IF EXISTS idx_auth_transactions_user_id;
DROP TABLE IF EXISTS auth_transactions;

DROP INDEX IF EXISTS idx_passkeys_user_id;
DROP INDEX IF EXISTS idx_passkeys_rp_credential;
DROP TABLE IF EXISTS passkeys;

ALTER TABLE user_sessions DROP COLUMN IF EXISTS mfa_verified_at;
ALTER TABLE user_sessions DROP COLUMN IF EXISTS mfa_method;
ALTER TABLE users DROP COLUMN IF EXISTS passkey_mfa_enabled;
