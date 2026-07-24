-- +goose Up
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS repository_names TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE container_registries DROP COLUMN IF EXISTS repository_names;
