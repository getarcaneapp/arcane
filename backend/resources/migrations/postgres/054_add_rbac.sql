-- +goose Up
CREATE TABLE IF NOT EXISTS roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    permissions JSONB NOT NULL DEFAULT '[]',
    built_in    BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS user_role_assignments (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'manual',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ura_user ON user_role_assignments(user_id);
CREATE INDEX IF NOT EXISTS idx_ura_role ON user_role_assignments(role_id);
CREATE INDEX IF NOT EXISTS idx_ura_env ON user_role_assignments(environment_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ura_uniq
    ON user_role_assignments(user_id, role_id, COALESCE(environment_id, ''));

CREATE TABLE IF NOT EXISTS api_key_permissions (
    id             TEXT PRIMARY KEY,
    api_key_id     TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    permission     TEXT NOT NULL,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_akp_key ON api_key_permissions(api_key_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_akp_uniq
    ON api_key_permissions(api_key_id, permission, COALESCE(environment_id, ''));

CREATE TABLE IF NOT EXISTS oidc_role_mappings (
    id             TEXT PRIMARY KEY,
    claim_value    TEXT NOT NULL,
    role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    -- 'manual' = created via UI/API; 'env' = declared via OIDC_ROLE_MAPPINGS
    -- env var and reconciled at boot. The API refuses mutations on 'env' rows.
    source         TEXT NOT NULL DEFAULT 'manual',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_orm_claim ON oidc_role_mappings(claim_value);

-- +goose Down
DELETE FROM settings WHERE key = 'oidcGroupsClaim';

DROP INDEX IF EXISTS idx_orm_claim;
DROP TABLE IF EXISTS oidc_role_mappings;

DROP INDEX IF EXISTS idx_akp_uniq;
DROP INDEX IF EXISTS idx_akp_key;
DROP TABLE IF EXISTS api_key_permissions;

DROP INDEX IF EXISTS idx_ura_uniq;
DROP INDEX IF EXISTS idx_ura_env;
DROP INDEX IF EXISTS idx_ura_role;
DROP INDEX IF EXISTS idx_ura_user;
DROP TABLE IF EXISTS user_role_assignments;

DROP TABLE IF EXISTS roles;
