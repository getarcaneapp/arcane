DROP INDEX IF EXISTS idx_projects_is_archived;
ALTER TABLE projects DROP COLUMN archived_at;
ALTER TABLE projects DROP COLUMN is_archived;
