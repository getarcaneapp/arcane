ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_dir_name_key;

DROP INDEX IF EXISTS idx_projects_path;
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_path_unique ON projects(path);
