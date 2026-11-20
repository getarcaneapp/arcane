package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v121SQLiteUp = `
CREATE TABLE IF NOT EXISTS project_backup_policies (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    stop_containers BOOLEAN NOT NULL DEFAULT true,
    local_enabled BOOLEAN NOT NULL DEFAULT true,
    s3_enabled BOOLEAN NOT NULL DEFAULT false,
    s3_destination_id TEXT NOT NULL DEFAULT '',
    volumes TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_project_backup_policies_project_id ON project_backup_policies(project_id);
ALTER TABLE project_backups ADD COLUMN policy_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_project_backups_policy_id ON project_backups(policy_id);
`
	v121PostgresUp = `
CREATE TABLE IF NOT EXISTS project_backup_policies (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    enabled boolean NOT NULL DEFAULT false,
    schedule text NOT NULL,
    retention_count integer NOT NULL DEFAULT 7,
    stop_containers boolean NOT NULL DEFAULT true,
    local_enabled boolean NOT NULL DEFAULT true,
    s3_enabled boolean NOT NULL DEFAULT false,
    s3_destination_id text NOT NULL DEFAULT '',
    volumes text,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS idx_project_backup_policies_project_id ON project_backup_policies(project_id);
ALTER TABLE project_backups ADD COLUMN IF NOT EXISTS policy_id text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_project_backups_policy_id ON project_backups(policy_id);
`
	v121SQLiteDown = `
DROP INDEX IF EXISTS idx_project_backups_policy_id;
ALTER TABLE project_backups DROP COLUMN policy_id;
DROP INDEX IF EXISTS idx_project_backup_policies_project_id;
DROP TABLE IF EXISTS project_backup_policies;
`
	v121PostgresDown = `
DROP INDEX IF EXISTS idx_project_backups_policy_id;
ALTER TABLE project_backups DROP COLUMN IF EXISTS policy_id;
DROP INDEX IF EXISTS idx_project_backup_policies_project_id;
DROP TABLE IF EXISTS project_backup_policies;
`
)

func init() {
	goose.AddMigrationContext(run(v121SQLiteUp, v121PostgresUp), run(v121SQLiteDown, v121PostgresDown))
}
