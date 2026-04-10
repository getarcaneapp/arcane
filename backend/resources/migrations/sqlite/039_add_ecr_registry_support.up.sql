-- Use create-copy-drop-rename for idempotent column additions in SQLite
-- (SQLite lacks ADD COLUMN IF NOT EXISTS).
DROP TABLE IF EXISTS container_registries_new;

CREATE TABLE container_registries_new (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    username TEXT NOT NULL,
    token TEXT NOT NULL,
    description TEXT,
    insecure BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    registry_type TEXT NOT NULL DEFAULT 'generic',
    aws_access_key_id TEXT,
    aws_secret_access_key TEXT,
    aws_region TEXT,
    ecr_token TEXT,
    ecr_token_generated_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

INSERT INTO container_registries_new (id, url, username, token, description, insecure, enabled, created_at, updated_at)
SELECT id, url, username, token, description, insecure, enabled, created_at, updated_at
FROM container_registries;

DROP TABLE container_registries;
ALTER TABLE container_registries_new RENAME TO container_registries;

CREATE INDEX IF NOT EXISTS idx_container_registries_enabled ON container_registries(enabled);
