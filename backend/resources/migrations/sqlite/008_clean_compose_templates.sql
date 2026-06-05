-- +goose Up
ALTER TABLE compose_templates DROP COLUMN meta_updated_at;

-- +goose Down
ALTER TABLE compose_templates ADD COLUMN meta_updated_at TEXT;
