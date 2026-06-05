-- +goose Up
-- Add directory sync support to gitops_syncs table
-- sync_directory: when true, syncs entire directory containing compose file (default: true)
-- synced_files: JSON array of file paths that were synced (for cleanup on updates)
-- max_sync_files: maximum number of files to sync (0 = unlimited)
-- max_sync_total_size: maximum total size in bytes (0 = unlimited)
-- max_sync_binary_size: maximum binary file size in bytes (0 = unlimited)

ALTER TABLE gitops_syncs ADD COLUMN sync_directory BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE gitops_syncs ADD COLUMN synced_files TEXT;
ALTER TABLE gitops_syncs ADD COLUMN max_sync_files INTEGER NOT NULL DEFAULT 500;
ALTER TABLE gitops_syncs ADD COLUMN max_sync_total_size BIGINT NOT NULL DEFAULT 52428800;
ALTER TABLE gitops_syncs ADD COLUMN max_sync_binary_size BIGINT NOT NULL DEFAULT 10485760;

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_sync_directory ON gitops_syncs(sync_directory);

-- +goose Down
-- Remove directory sync columns from gitops_syncs table

DROP INDEX IF EXISTS idx_gitops_syncs_sync_directory;

ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS max_sync_binary_size;
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS max_sync_total_size;
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS max_sync_files;
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS synced_files;
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS sync_directory;
