-- +goose Up
CREATE TABLE system_backup_runs (
    id TEXT PRIMARY KEY,
    size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    status TEXT NOT NULL,
    trigger TEXT NOT NULL,
    destination TEXT NOT NULL,
    local_snapshot_id TEXT,
    remote_snapshot_id TEXT,
    s3_destination_id TEXT,
    policy_id TEXT,
    error TEXT
);
CREATE INDEX idx_system_backup_runs_s3_destination_id ON system_backup_runs(s3_destination_id);
CREATE INDEX idx_system_backup_runs_policy_id ON system_backup_runs(policy_id);

CREATE TABLE system_backup_policies (
    id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
CREATE INDEX idx_system_backup_policies_s3_destination_id ON system_backup_policies(s3_destination_id);

CREATE TABLE system_backup_recovery_config (
    id TEXT PRIMARY KEY,
    encrypted_recovery_key TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

-- +goose Down
DROP TABLE system_backup_recovery_config;
DROP TABLE system_backup_policies;
DROP TABLE system_backup_runs;
