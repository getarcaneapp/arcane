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
