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
    use_ssl INTEGER NOT NULL DEFAULT 1,
    force_path_style INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
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
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies(s3_destination_id);

ALTER TABLE volume_backup_policies ADD COLUMN stop_containers INTEGER NOT NULL DEFAULT 0;
ALTER TABLE volume_backups ADD COLUMN destination TEXT NOT NULL DEFAULT 'local';
UPDATE volume_backups SET destination = 'local_s3' WHERE COALESCE(remote_key, '') <> '';

DELETE FROM volume_backups;
ALTER TABLE volume_backups ADD COLUMN local_snapshot_id TEXT;
ALTER TABLE volume_backups ADD COLUMN remote_snapshot_id TEXT;
ALTER TABLE volume_backups ADD COLUMN policy_id TEXT;
CREATE INDEX idx_volume_backups_policy_id ON volume_backups(policy_id);

CREATE TABLE volume_backup_policies_new (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    stop_containers INTEGER NOT NULL DEFAULT 0
);
INSERT INTO volume_backup_policies_new SELECT * FROM volume_backup_policies;
DROP TABLE volume_backup_policies;
ALTER TABLE volume_backup_policies_new RENAME TO volume_backup_policies;
CREATE INDEX idx_volume_backup_policies_volume_name ON volume_backup_policies(volume_name);
CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies(s3_destination_id);

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

DELETE FROM volume_backup_policies
WHERE id NOT IN (SELECT MIN(id) FROM volume_backup_policies GROUP BY volume_name);
CREATE TABLE volume_backup_policies_old (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    stop_containers INTEGER NOT NULL DEFAULT 0
);
INSERT INTO volume_backup_policies_old SELECT * FROM volume_backup_policies;
DROP TABLE volume_backup_policies;
ALTER TABLE volume_backup_policies_old RENAME TO volume_backup_policies;
CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies(s3_destination_id);
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
