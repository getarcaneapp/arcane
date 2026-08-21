-- +goose Up
ALTER TABLE gitops_syncs ADD COLUMN pull_image_after_sync BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE gitops_syncs DROP COLUMN pull_image_after_sync;
