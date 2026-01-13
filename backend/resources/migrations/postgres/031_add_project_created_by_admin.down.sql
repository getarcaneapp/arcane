DROP INDEX IF EXISTS idx_projects_created_by_admin;
ALTER TABLE projects DROP COLUMN created_by_admin;
