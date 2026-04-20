ALTER TABLE gitops_syncs ADD COLUMN target_type VARCHAR(255) NOT NULL DEFAULT 'project';
