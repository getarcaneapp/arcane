-- Security fix: Track whether project was created by an admin user
-- Lifecycle hooks are only executed for projects created by admins
ALTER TABLE projects ADD COLUMN created_by_admin INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_projects_created_by_admin ON projects(created_by_admin);
