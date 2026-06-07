-- Recreate legacy cache table
CREATE TABLE IF NOT EXISTS project_cache (
  id TEXT PRIMARY KEY,
  created_at DATETIME,
  updated_at DATETIME,
  deleted_at DATETIME,
  stack_id TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  service_count INTEGER NOT NULL DEFAULT 0,
  running_count INTEGER NOT NULL DEFAULT 0,
  auto_update INTEGER NOT NULL DEFAULT 0,
  last_modified DATETIME,
  compose_hash TEXT NOT NULL DEFAULT '',
  cached_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_cache_stack_id ON project_cache(stack_id);

-- Rename projects -> stacks
ALTER TABLE projects RENAME TO stacks;