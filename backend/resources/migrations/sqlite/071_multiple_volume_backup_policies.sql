-- +goose Up
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

-- +goose Down
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
