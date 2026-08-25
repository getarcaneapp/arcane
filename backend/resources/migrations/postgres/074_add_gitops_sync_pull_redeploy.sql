-- +goose Up
ALTER TABLE gitops_syncs ADD COLUMN pull_image_after_sync BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gitops_syncs ADD COLUMN redeploy_after_sync BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS redeploy_after_sync;
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS pull_image_after_sync;
