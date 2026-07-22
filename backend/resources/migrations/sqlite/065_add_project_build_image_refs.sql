-- +goose Up
ALTER TABLE projects ADD COLUMN build_image_refs_json TEXT;

-- +goose Down
ALTER TABLE projects DROP COLUMN build_image_refs_json;
