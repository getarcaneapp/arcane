-- Use create-copy-drop-rename to remove ECR columns in SQLite
-- (SQLite lacks DROP COLUMN IF EXISTS).
DROP TABLE IF EXISTS container_registries_old;

CREATE TABLE container_registries_old (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    username TEXT NOT NULL,
    token TEXT NOT NULL,
    description TEXT,
    insecure BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

INSERT INTO container_registries_old (id, url, username, token, description, insecure, enabled, created_at, updated_at)
SELECT id, url, username, token, description, insecure, enabled, created_at, updated_at
FROM container_registries;

DROP TABLE container_registries;
ALTER TABLE container_registries_old RENAME TO container_registries;

CREATE INDEX IF NOT EXISTS idx_container_registries_enabled ON container_registries(enabled);
