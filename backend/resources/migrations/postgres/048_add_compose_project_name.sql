-- +goose Up
ALTER TABLE projects ADD COLUMN compose_project_name TEXT;

-- +goose Down
ALTER TABLE projects DROP COLUMN compose_project_name;
