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
ALTER TABLE volume_backups ADD COLUMN s3_destination_id TEXT;
ALTER TABLE volume_backups ADD COLUMN error TEXT;
ALTER TABLE volume_backups ADD COLUMN destination TEXT NOT NULL DEFAULT 'local';
ALTER TABLE volume_backups ADD COLUMN local_snapshot_id TEXT;
ALTER TABLE volume_backups ADD COLUMN remote_snapshot_id TEXT;
ALTER TABLE volume_backups ADD COLUMN policy_id TEXT;
-- Pre-existing rows are tar.gz archive backups; new Rustic-snapshot rows set 'rustic'.
ALTER TABLE volume_backups ADD COLUMN format TEXT NOT NULL DEFAULT 'archive';
CREATE INDEX idx_volume_backups_s3_destination_id ON volume_backups(s3_destination_id);
CREATE INDEX idx_volume_backups_policy_id ON volume_backups(policy_id);

CREATE TABLE volume_backup_policies (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    stop_containers BOOLEAN NOT NULL DEFAULT FALSE,
    local_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    s3_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    s3_destination_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);
CREATE INDEX idx_volume_backup_policies_volume_name ON volume_backup_policies(volume_name);
CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies(s3_destination_id);

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
-- Refuse to downgrade while Rustic-format backups exist: the pre-073 schema cannot
-- represent them and their snapshots would become unreachable.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM volume_backups WHERE format <> 'archive') THEN
        RAISE EXCEPTION 'downgrade blocked: Rustic-format volume backups exist; delete them first or stay on this version';
    END IF;
END
$$;
-- +goose StatementEnd

DROP TABLE system_backup_recovery_config;
DROP TABLE system_backup_policies;
DROP TABLE system_backup_runs;
DROP TABLE volume_backup_policies;

DROP INDEX idx_volume_backups_policy_id;
DROP INDEX idx_volume_backups_s3_destination_id;
ALTER TABLE volume_backups DROP COLUMN format;
ALTER TABLE volume_backups DROP COLUMN policy_id;
ALTER TABLE volume_backups DROP COLUMN remote_snapshot_id;
ALTER TABLE volume_backups DROP COLUMN local_snapshot_id;
ALTER TABLE volume_backups DROP COLUMN destination;
ALTER TABLE volume_backups DROP COLUMN error;
ALTER TABLE volume_backups DROP COLUMN s3_destination_id;
ALTER TABLE volume_backups DROP COLUMN trigger;
ALTER TABLE volume_backups DROP COLUMN status;
DROP TABLE s3_destinations;
