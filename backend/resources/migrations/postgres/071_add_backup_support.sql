-- +goose Up
CREATE TABLE s3_destinations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    endpoint TEXT,
    bucket TEXT NOT NULL,
    region TEXT NOT NULL,
    access_key_id TEXT NOT NULL,
    secret_access_key TEXT NOT NULL,
    prefix TEXT,
    use_ssl BOOLEAN NOT NULL DEFAULT TRUE,
    force_path_style BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

ALTER TABLE volume_backups ADD COLUMN status TEXT NOT NULL DEFAULT 'succeeded';
ALTER TABLE volume_backups ADD COLUMN trigger TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE volume_backups ADD COLUMN remote_key TEXT;
ALTER TABLE volume_backups ADD COLUMN s3_destination_id TEXT;
ALTER TABLE volume_backups ADD COLUMN error TEXT;
CREATE INDEX idx_volume_backups_s3_destination_id ON volume_backups(s3_destination_id);

CREATE TABLE volume_backup_policies (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL UNIQUE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    s3_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    s3_destination_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);
CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies(s3_destination_id);

ALTER TABLE volume_backup_policies ADD COLUMN stop_containers BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE volume_backups ADD COLUMN destination TEXT NOT NULL DEFAULT 'local';
UPDATE volume_backups SET destination = 'local_s3' WHERE COALESCE(remote_key, '') <> '';

DELETE FROM volume_backups;
ALTER TABLE volume_backups ADD COLUMN local_snapshot_id TEXT;
ALTER TABLE volume_backups ADD COLUMN remote_snapshot_id TEXT;
ALTER TABLE volume_backups ADD COLUMN policy_id TEXT;
CREATE INDEX idx_volume_backups_policy_id ON volume_backups(policy_id);
ALTER TABLE volume_backup_policies DROP CONSTRAINT volume_backup_policies_volume_name_key;
CREATE INDEX idx_volume_backup_policies_volume_name ON volume_backup_policies(volume_name);

CREATE TABLE system_backup_runs (
    id TEXT PRIMARY KEY,
    size BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ,
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
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    s3_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    s3_destination_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);
CREATE INDEX idx_system_backup_policies_s3_destination_id ON system_backup_policies(s3_destination_id);

CREATE TABLE system_backup_recovery_config (
    id TEXT PRIMARY KEY,
    encrypted_recovery_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

-- +goose Down
DROP TABLE system_backup_recovery_config;
DROP TABLE system_backup_policies;
DROP TABLE system_backup_runs;

DELETE FROM volume_backup_policies a
USING volume_backup_policies b
WHERE a.volume_name = b.volume_name AND a.id > b.id;
DROP INDEX idx_volume_backup_policies_volume_name;
ALTER TABLE volume_backup_policies ADD CONSTRAINT volume_backup_policies_volume_name_key UNIQUE (volume_name);
DROP INDEX idx_volume_backups_policy_id;
ALTER TABLE volume_backups DROP COLUMN policy_id;

DELETE FROM volume_backups;
ALTER TABLE volume_backups DROP COLUMN remote_snapshot_id;
ALTER TABLE volume_backups DROP COLUMN local_snapshot_id;
ALTER TABLE volume_backups DROP COLUMN destination;
ALTER TABLE volume_backup_policies DROP COLUMN stop_containers;

DROP TABLE volume_backup_policies;
DROP INDEX idx_volume_backups_s3_destination_id;
ALTER TABLE volume_backups DROP COLUMN error;
ALTER TABLE volume_backups DROP COLUMN s3_destination_id;
ALTER TABLE volume_backups DROP COLUMN remote_key;
ALTER TABLE volume_backups DROP COLUMN trigger;
ALTER TABLE volume_backups DROP COLUMN status;
DROP TABLE s3_destinations;
