-- +goose Up
ALTER TABLE gitops_syncs ADD COLUMN mode TEXT NOT NULL DEFAULT 'deploy';
ALTER TABLE gitops_syncs ADD COLUMN backup_directory TEXT NOT NULL DEFAULT '';
ALTER TABLE gitops_syncs ADD COLUMN backup_paths TEXT;
ALTER TABLE gitops_syncs ADD COLUMN backup_on_save BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE gitops_syncs ADD COLUMN backup_pending BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gitops_syncs ADD COLUMN backup_pending_since DATETIME;
ALTER TABLE gitops_syncs ADD COLUMN backup_conflict BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gitops_syncs ADD COLUMN backup_failure_reason TEXT;
ALTER TABLE gitops_syncs ADD COLUMN last_backup_at DATETIME;
ALTER TABLE gitops_syncs ADD COLUMN last_backup_snapshot TEXT;
CREATE UNIQUE INDEX idx_gitops_syncs_backup_project ON gitops_syncs(project_id) WHERE mode = 'backup';

-- +goose Down
DROP INDEX idx_gitops_syncs_backup_project;
ALTER TABLE gitops_syncs DROP COLUMN last_backup_snapshot;
ALTER TABLE gitops_syncs DROP COLUMN last_backup_at;
ALTER TABLE gitops_syncs DROP COLUMN backup_failure_reason;
ALTER TABLE gitops_syncs DROP COLUMN backup_conflict;
ALTER TABLE gitops_syncs DROP COLUMN backup_pending_since;
ALTER TABLE gitops_syncs DROP COLUMN backup_pending;
ALTER TABLE gitops_syncs DROP COLUMN backup_on_save;
ALTER TABLE gitops_syncs DROP COLUMN backup_paths;
ALTER TABLE gitops_syncs DROP COLUMN backup_directory;
ALTER TABLE gitops_syncs DROP COLUMN mode;
