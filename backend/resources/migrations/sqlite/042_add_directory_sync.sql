-- +goose Up
-- Add directory sync support to gitops_syncs table
-- sync_directory: when true, syncs entire directory containing compose file (default: true)
-- synced_files: JSON array of file paths that were synced (for cleanup on updates)
-- max_sync_files: maximum number of files to sync (0 = unlimited)
-- max_sync_total_size: maximum total size in bytes (0 = unlimited)
-- max_sync_binary_size: maximum binary file size in bytes (0 = unlimited)

ALTER TABLE gitops_syncs ADD COLUMN sync_directory INTEGER NOT NULL DEFAULT 1;
ALTER TABLE gitops_syncs ADD COLUMN synced_files TEXT;
ALTER TABLE gitops_syncs ADD COLUMN max_sync_files INTEGER NOT NULL DEFAULT 500;
ALTER TABLE gitops_syncs ADD COLUMN max_sync_total_size INTEGER NOT NULL DEFAULT 52428800;
ALTER TABLE gitops_syncs ADD COLUMN max_sync_binary_size INTEGER NOT NULL DEFAULT 10485760;

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_sync_directory ON gitops_syncs(sync_directory);

-- +goose Down
-- Remove directory sync columns from gitops_syncs table
-- SQLite doesn't support DROP COLUMN in older versions, so we recreate the table

DROP INDEX IF EXISTS idx_gitops_syncs_sync_directory;

CREATE TABLE gitops_syncs_backup AS SELECT
    id,
    name,
    environment_id,
    repository_id,
    branch,
    compose_path,
    project_name,
    project_id,
    auto_sync,
    sync_interval,
    last_sync_at,
    last_sync_status,
    last_sync_error,
    last_sync_commit,
    created_at,
    updated_at
FROM gitops_syncs;

DROP TABLE gitops_syncs;

CREATE TABLE gitops_syncs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    branch TEXT NOT NULL,
    compose_path TEXT NOT NULL,
    project_name TEXT NOT NULL,
    project_id TEXT,
    auto_sync INTEGER NOT NULL DEFAULT 0,
    sync_interval INTEGER NOT NULL DEFAULT 60,
    last_sync_at DATETIME,
    last_sync_status TEXT,
    last_sync_error TEXT,
    last_sync_commit TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE,
    FOREIGN KEY (repository_id) REFERENCES git_repositories(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);

INSERT INTO gitops_syncs SELECT * FROM gitops_syncs_backup;
DROP TABLE gitops_syncs_backup;

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_environment_id ON gitops_syncs(environment_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_repository_id ON gitops_syncs(repository_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_project_id ON gitops_syncs(project_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_auto_sync ON gitops_syncs(auto_sync);
