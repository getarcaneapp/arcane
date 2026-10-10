-- +goose Up
ALTER TABLE compose_templates ADD COLUMN meta_registry_icon_url TEXT;

-- +goose Down
ALTER TABLE compose_templates DROP COLUMN meta_registry_icon_url;
