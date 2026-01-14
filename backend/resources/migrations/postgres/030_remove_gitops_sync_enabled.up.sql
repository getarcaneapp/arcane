-- Remove the enabled column from gitops_syncs table
-- Only autoSync field will control automatic syncing behavior
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS enabled;
