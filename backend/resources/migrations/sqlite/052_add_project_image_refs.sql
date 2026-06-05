-- +goose Up
ALTER TABLE projects ADD COLUMN image_refs_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE projects DROP COLUMN image_refs_json;
