-- Remove gitops_managed_by column from projects table
ALTER TABLE projects DROP COLUMN IF EXISTS gitops_managed_by;

DROP TABLE IF EXISTS gitops_syncs;
DROP TABLE IF EXISTS git_repositories;
