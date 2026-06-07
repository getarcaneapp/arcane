-- +goose Up
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_dir_name_key;

DROP INDEX IF EXISTS idx_projects_path;
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_path_unique ON projects(path);

-- +goose Down
DROP INDEX IF EXISTS idx_projects_path_unique;
CREATE INDEX IF NOT EXISTS idx_projects_path ON projects(path);

-- Rollback is only safe when dir_name values remain unique.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM projects
        WHERE dir_name IS NOT NULL
        GROUP BY dir_name
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'Cannot rollback migration 044: duplicate projects.dir_name values exist. Remove duplicates before running the down migration.';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE projects
ADD CONSTRAINT projects_dir_name_key UNIQUE (dir_name);
