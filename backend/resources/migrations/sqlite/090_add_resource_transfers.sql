-- +goose Up
CREATE TABLE IF NOT EXISTS resource_transfers (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    mode TEXT NOT NULL,
    source_environment_id TEXT NOT NULL,
    destination_environment_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    phase TEXT NOT NULL DEFAULT 'pending',
    attempt INTEGER NOT NULL DEFAULT 0,
    plan TEXT NOT NULL DEFAULT '{}',
    resources TEXT NOT NULL DEFAULT '[]',
    recorded_consumers TEXT NOT NULL DEFAULT '[]',
    destination_project_id TEXT NOT NULL DEFAULT '',
    destination_startup_attempted BOOLEAN NOT NULL DEFAULT false,
    destination_startup_at DATETIME,
    source_held BOOLEAN NOT NULL DEFAULT false,
    cancel_requested BOOLEAN NOT NULL DEFAULT false,
    recovery TEXT,
    activity_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    requested_by TEXT NOT NULL DEFAULT '',
    finished_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_resource_transfers_idempotency_key ON resource_transfers(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_resource_transfers_status ON resource_transfers(status);
CREATE INDEX IF NOT EXISTS idx_resource_transfers_source_environment ON resource_transfers(source_environment_id);

-- +goose Down
DROP INDEX IF EXISTS idx_resource_transfers_source_environment;
DROP INDEX IF EXISTS idx_resource_transfers_status;
DROP INDEX IF EXISTS idx_resource_transfers_idempotency_key;
DROP TABLE IF EXISTS resource_transfers;
