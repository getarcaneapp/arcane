-- +goose Up
-- Add api_key_id column to environments table for API key-based pairing
ALTER TABLE environments ADD COLUMN api_key_id TEXT REFERENCES api_keys(id) ON DELETE SET NULL;

-- Add environment_id column to api_keys table to link API keys to environments
ALTER TABLE api_keys ADD COLUMN environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE;

-- +goose Down
-- Rebuilding removes the table-level environment FK introduced by later migrations.
ALTER TABLE environments DROP COLUMN api_key_id;

CREATE TABLE api_keys_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    key_hash TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    user_id TEXT NOT NULL,
    expires_at DATETIME,
    last_used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

INSERT INTO api_keys_new (id, name, description, key_hash, key_prefix, user_id, expires_at, last_used_at, created_at, updated_at)
SELECT id, name, description, key_hash, key_prefix, user_id, expires_at, last_used_at, created_at, updated_at FROM api_keys;

DROP TABLE api_keys;
ALTER TABLE api_keys_new RENAME TO api_keys;

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_prefix ON api_keys(key_prefix);
