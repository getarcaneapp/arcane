-- SQLite doesn't support DROP COLUMN directly, need to recreate table
CREATE TABLE projects_backup AS SELECT id, name, dir_name, path, status, status_reason, service_count, running_count, gitops_managed_by, created_at, updated_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_backup RENAME TO projects;
