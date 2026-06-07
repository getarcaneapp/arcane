-- Remove gitops_managed_by column from projects table
-- Note: SQLite doesn't support DROP COLUMN directly, so we'll leave it
-- or need to recreate the table if this is critical

DROP TABLE IF EXISTS gitops_syncs;
DROP TABLE IF EXISTS git_repositories;
