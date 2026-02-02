-- Remove directory sync columns from gitops_syncs table

DROP INDEX IF EXISTS idx_gitops_syncs_sync_directory;

ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS sync_directory;
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS synced_files;
