CREATE TABLE IF NOT EXISTS secrets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    environment_id TEXT NOT NULL DEFAULT '0',
    content TEXT NOT NULL,
    file_path TEXT,
    description TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    UNIQUE(name, environment_id)
);

CREATE INDEX IF NOT EXISTS idx_secrets_environment_id ON secrets(environment_id);
CREATE INDEX IF NOT EXISTS idx_secrets_name ON secrets(name);
