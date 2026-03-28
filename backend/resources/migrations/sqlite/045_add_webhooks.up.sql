CREATE TABLE IF NOT EXISTS webhooks (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    token_hash       TEXT NOT NULL UNIQUE,
    token_prefix     TEXT NOT NULL,
    target_type      TEXT NOT NULL,
    target_id        TEXT NOT NULL,
    environment_id   TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1,
    last_triggered_at DATETIME,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME
);

CREATE INDEX IF NOT EXISTS idx_webhooks_token_prefix ON webhooks(token_prefix);
CREATE INDEX IF NOT EXISTS idx_webhooks_environment_id ON webhooks(environment_id);
