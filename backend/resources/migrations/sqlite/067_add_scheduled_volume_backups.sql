-- +goose Up
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

-- +goose Down
DROP TABLE volume_backup_policies;
DROP INDEX idx_volume_backups_s3_destination_id;
ALTER TABLE volume_backups DROP COLUMN error;
ALTER TABLE volume_backups DROP COLUMN s3_destination_id;
ALTER TABLE volume_backups DROP COLUMN remote_key;
ALTER TABLE volume_backups DROP COLUMN trigger;
ALTER TABLE volume_backups DROP COLUMN status;
