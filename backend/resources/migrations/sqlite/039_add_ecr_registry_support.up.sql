-- SQLite does not support ADD COLUMN IF NOT EXISTS.
-- Using CREATE TABLE + INSERT + DROP + RENAME for idempotency.
CREATE TABLE IF NOT EXISTS container_registries_v039 (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL DEFAULT '',
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

INSERT OR IGNORE INTO container_registries_v039 (id, url, username, token, description, insecure, enabled, created_at, updated_at)
SELECT id, url, username, token, description, insecure, enabled, created_at, updated_at
FROM container_registries
WHERE NOT EXISTS (SELECT 1 FROM container_registries_v039);

DROP TABLE IF EXISTS container_registries;

ALTER TABLE container_registries_v039 RENAME TO container_registries;
