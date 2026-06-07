BEGIN;
-- Rename stacks -> projects
ALTER TABLE IF EXISTS stacks RENAME TO projects;

-- Remove legacy cache table
DROP TABLE IF EXISTS project_cache;
COMMIT;