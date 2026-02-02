-- Add directory sync support to gitops_syncs table
-- sync_directory: when true, syncs entire directory containing compose file (default: true)
-- synced_files: JSON array of file paths that were synced (for cleanup on updates)

ALTER TABLE gitops_syncs ADD COLUMN sync_directory BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE gitops_syncs ADD COLUMN synced_files TEXT;

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_sync_directory ON gitops_syncs(sync_directory);
