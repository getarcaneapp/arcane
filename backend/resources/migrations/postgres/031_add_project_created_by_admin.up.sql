-- Security fix: Track whether project was created by an admin user
-- Lifecycle hooks are only executed for projects created by admins
ALTER TABLE projects ADD COLUMN created_by_admin BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX idx_projects_created_by_admin ON projects(created_by_admin);
