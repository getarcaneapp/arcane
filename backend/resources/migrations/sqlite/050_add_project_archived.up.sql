ALTER TABLE projects ADD COLUMN is_archived BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN archived_at DATETIME;
CREATE INDEX idx_projects_is_archived ON projects(is_archived);
