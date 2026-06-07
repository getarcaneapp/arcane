-- +goose Up
ALTER TABLE gitops_syncs ADD COLUMN target_type TEXT NOT NULL DEFAULT 'project';

-- +goose Down
ALTER TABLE gitops_syncs DROP COLUMN target_type;
