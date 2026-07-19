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

-- +goose Down
DROP TABLE volume_backup_policies;
DROP INDEX idx_volume_backups_s3_destination_id;
ALTER TABLE volume_backups DROP COLUMN error;
ALTER TABLE volume_backups DROP COLUMN s3_destination_id;
ALTER TABLE volume_backups DROP COLUMN remote_key;
ALTER TABLE volume_backups DROP COLUMN trigger;
ALTER TABLE volume_backups DROP COLUMN status;
