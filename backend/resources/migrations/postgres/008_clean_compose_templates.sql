-- +goose Up
ALTER TABLE compose_templates 
  DROP COLUMN IF EXISTS meta_updated_at;

-- +goose Down
ALTER TABLE compose_templates
  ADD COLUMN IF NOT EXISTS meta_updated_at TEXT;
