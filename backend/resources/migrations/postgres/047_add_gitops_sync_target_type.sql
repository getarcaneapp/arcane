-- +goose Up
ALTER TABLE gitops_syncs ADD COLUMN target_type VARCHAR(255) NOT NULL DEFAULT 'project';

-- +goose Down
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS target_type;
