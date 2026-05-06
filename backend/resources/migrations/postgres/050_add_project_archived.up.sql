ALTER TABLE projects ADD COLUMN is_archived BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE projects ADD COLUMN archived_at TIMESTAMP;
CREATE INDEX idx_projects_is_archived ON projects(is_archived);
