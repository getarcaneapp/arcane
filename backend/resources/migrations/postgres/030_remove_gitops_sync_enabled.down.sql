-- Re-add the enabled column to gitops_syncs table
ALTER TABLE gitops_syncs ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true;
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_enabled ON gitops_syncs(enabled);
