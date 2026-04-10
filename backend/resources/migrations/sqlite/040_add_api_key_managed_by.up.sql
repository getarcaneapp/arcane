-- Use create-copy-drop-rename for idempotent column additions in SQLite
-- (SQLite lacks ADD COLUMN IF NOT EXISTS).
DROP TABLE IF EXISTS api_keys_new;

CREATE TABLE api_keys_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    key_hash TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    managed_by TEXT,
    user_id TEXT NOT NULL,
    environment_id TEXT,
    expires_at DATETIME,
    last_used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

INSERT INTO api_keys_new (
    id,
    name,
    description,
    key_hash,
    key_prefix,
    user_id,
    environment_id,
    expires_at,
    last_used_at,
    created_at,
    updated_at
)
SELECT
    id,
    name,
    description,
    key_hash,
    key_prefix,
    user_id,
    environment_id,
    expires_at,
    last_used_at,
    created_at,
    updated_at
FROM api_keys;

DROP TABLE api_keys;
ALTER TABLE api_keys_new RENAME TO api_keys;

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_prefix ON api_keys(key_prefix);
