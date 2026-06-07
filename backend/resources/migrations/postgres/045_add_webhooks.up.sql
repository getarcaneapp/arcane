CREATE TABLE IF NOT EXISTS webhooks (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    token_hash       TEXT NOT NULL UNIQUE,
    token_prefix     TEXT NOT NULL,
    target_type      TEXT NOT NULL,
    action_type      TEXT NOT NULL DEFAULT '',
    target_id        TEXT NOT NULL,
    target_ref       TEXT NOT NULL DEFAULT '',
    environment_id   TEXT NOT NULL DEFAULT '',
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    last_triggered_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_webhooks_token_prefix ON webhooks (token_prefix);
CREATE INDEX IF NOT EXISTS idx_webhooks_environment_id ON webhooks (environment_id);
