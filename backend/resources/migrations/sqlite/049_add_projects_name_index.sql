-- +goose Up
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects (name);

-- +goose Down
DROP INDEX IF EXISTS idx_projects_name;
