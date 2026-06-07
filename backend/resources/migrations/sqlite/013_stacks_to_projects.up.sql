-- Rename stacks -> projects
ALTER TABLE stacks RENAME TO projects;

-- Remove legacy cache table
DROP TABLE IF EXISTS project_cache;