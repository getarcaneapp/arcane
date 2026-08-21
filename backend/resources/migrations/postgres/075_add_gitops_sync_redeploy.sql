-- +goose Up
ALTER TABLE gitops_syncs ADD COLUMN redeploy_after_sync BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS redeploy_after_sync;
