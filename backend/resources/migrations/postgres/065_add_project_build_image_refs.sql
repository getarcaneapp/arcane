-- +goose Up
ALTER TABLE projects ADD COLUMN IF NOT EXISTS build_image_refs_json TEXT;

-- +goose Down
ALTER TABLE projects DROP COLUMN IF EXISTS build_image_refs_json;
