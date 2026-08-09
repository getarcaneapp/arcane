-- +goose Up
DROP INDEX IF EXISTS idx_projects_status;
ALTER TABLE projects DROP COLUMN status;
ALTER TABLE projects DROP COLUMN running_count;

-- +goose Down
ALTER TABLE projects ADD COLUMN status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE projects ADD COLUMN running_count INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);
