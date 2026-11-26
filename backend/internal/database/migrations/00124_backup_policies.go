package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v124SQLiteUp = `
CREATE TABLE IF NOT EXISTS backup_policies (
    id TEXT PRIMARY KEY,
    subject_kind TEXT NOT NULL,
    subject_id TEXT NOT NULL DEFAULT '',
    environment_id TEXT NOT NULL DEFAULT '0',
    enabled BOOLEAN NOT NULL DEFAULT false,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    stop_containers BOOLEAN NOT NULL DEFAULT false,
    local_enabled BOOLEAN NOT NULL DEFAULT true,
    s3_enabled BOOLEAN NOT NULL DEFAULT false,
    s3_destination_id TEXT NOT NULL DEFAULT '',
    options TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_backup_policies_subject ON backup_policies(subject_kind, subject_id, environment_id);
CREATE INDEX IF NOT EXISTS idx_backup_policies_s3_destination_id ON backup_policies(s3_destination_id);
INSERT INTO backup_policies (
    id, subject_kind, subject_id, environment_id, enabled, schedule, retention_count, stop_containers, local_enabled,
    s3_enabled, s3_destination_id, options, created_at, updated_at
)
SELECT id, 'volume', volume_name, COALESCE(NULLIF(environment_id, ''), '0'), enabled, schedule, retention_count, stop_containers,
    local_enabled, s3_enabled, COALESCE(s3_destination_id, ''), '{}', created_at, updated_at
FROM volume_backup_policies;
DROP TABLE volume_backup_policies;
ALTER TABLE project_backups ADD COLUMN policy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE project_backups ADD COLUMN orphaned_at TIMESTAMP;
CREATE INDEX IF NOT EXISTS idx_project_backups_policy_id ON project_backups(policy_id);
`
	v124PostgresUp = `
CREATE TABLE IF NOT EXISTS backup_policies (
    id text PRIMARY KEY,
    subject_kind text NOT NULL,
    subject_id text NOT NULL DEFAULT '',
    environment_id text NOT NULL DEFAULT '0',
    enabled boolean NOT NULL DEFAULT false,
    schedule text NOT NULL,
    retention_count integer NOT NULL DEFAULT 7,
    stop_containers boolean NOT NULL DEFAULT false,
    local_enabled boolean NOT NULL DEFAULT true,
    s3_enabled boolean NOT NULL DEFAULT false,
    s3_destination_id text NOT NULL DEFAULT '',
    options text NOT NULL DEFAULT '{}',
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS idx_backup_policies_subject ON backup_policies(subject_kind, subject_id, environment_id);
CREATE INDEX IF NOT EXISTS idx_backup_policies_s3_destination_id ON backup_policies(s3_destination_id);
INSERT INTO backup_policies (
    id, subject_kind, subject_id, environment_id, enabled, schedule, retention_count, stop_containers, local_enabled,
    s3_enabled, s3_destination_id, options, created_at, updated_at
)
SELECT id, 'volume', volume_name, COALESCE(NULLIF(environment_id, ''), '0'), enabled, schedule, retention_count, stop_containers,
    local_enabled, s3_enabled, COALESCE(s3_destination_id, ''), '{}', created_at, updated_at
FROM volume_backup_policies;
DROP TABLE volume_backup_policies;
ALTER TABLE project_backups ADD COLUMN IF NOT EXISTS policy_id text NOT NULL DEFAULT '';
ALTER TABLE project_backups ADD COLUMN IF NOT EXISTS orphaned_at timestamp with time zone;
CREATE INDEX IF NOT EXISTS idx_project_backups_policy_id ON project_backups(policy_id);
`
	v124SQLiteDown = `
DROP INDEX IF EXISTS idx_project_backups_policy_id;
ALTER TABLE project_backups DROP COLUMN orphaned_at;
ALTER TABLE project_backups DROP COLUMN policy_id;
CREATE TABLE volume_backup_policies (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    environment_id TEXT NOT NULL DEFAULT '0',
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    stop_containers INTEGER NOT NULL DEFAULT 0,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
CREATE INDEX idx_volume_backup_policies_volume_name ON volume_backup_policies(volume_name);
CREATE INDEX idx_volume_backup_policies_environment_id ON volume_backup_policies(environment_id);
CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies(s3_destination_id);
INSERT INTO volume_backup_policies (
    id, volume_name, environment_id, enabled, schedule, retention_count, stop_containers, local_enabled, s3_enabled,
    s3_destination_id, created_at, updated_at
)
SELECT id, subject_id, environment_id, enabled, schedule, retention_count, stop_containers, local_enabled, s3_enabled,
    s3_destination_id, created_at, updated_at
FROM backup_policies WHERE subject_kind = 'volume';
DROP TABLE backup_policies;
`
	v124PostgresDown = `
DROP INDEX IF EXISTS idx_project_backups_policy_id;
ALTER TABLE project_backups DROP COLUMN IF EXISTS orphaned_at;
ALTER TABLE project_backups DROP COLUMN IF EXISTS policy_id;
CREATE TABLE volume_backup_policies (
    id text NOT NULL,
    volume_name text NOT NULL,
    environment_id text NOT NULL DEFAULT '0',
    enabled boolean DEFAULT false NOT NULL,
    schedule text NOT NULL,
    retention_count integer DEFAULT 7 NOT NULL,
    stop_containers boolean DEFAULT false NOT NULL,
    local_enabled boolean DEFAULT true NOT NULL,
    s3_enabled boolean DEFAULT false NOT NULL,
    s3_destination_id text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    CONSTRAINT volume_backup_policies_pkey PRIMARY KEY (id)
);
CREATE INDEX idx_volume_backup_policies_volume_name ON volume_backup_policies USING btree (volume_name);
CREATE INDEX idx_volume_backup_policies_environment_id ON volume_backup_policies USING btree (environment_id);
CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies USING btree (s3_destination_id);
INSERT INTO volume_backup_policies (
    id, volume_name, environment_id, enabled, schedule, retention_count, stop_containers, local_enabled, s3_enabled,
    s3_destination_id, created_at, updated_at
)
SELECT id, subject_id, environment_id, enabled, schedule, retention_count, stop_containers, local_enabled, s3_enabled,
    s3_destination_id, created_at, updated_at
FROM backup_policies WHERE subject_kind = 'volume';
DROP TABLE backup_policies;
`
)

func init() {
	goose.AddMigrationContext(run(v124SQLiteUp, v124PostgresUp), run(v124SQLiteDown, v124PostgresDown))
}
