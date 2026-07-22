-- +goose Up
ALTER TABLE container_registries ADD COLUMN repository_names TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE container_registries DROP COLUMN repository_names;
