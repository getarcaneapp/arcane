-- +goose Up
ALTER TABLE compose_templates ADD COLUMN IF NOT EXISTS meta_registry_icon_url TEXT;

-- +goose Down
ALTER TABLE compose_templates DROP COLUMN IF EXISTS meta_registry_icon_url;
