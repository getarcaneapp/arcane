BEGIN;
-- Recreate legacy cache table
CREATE TABLE IF NOT EXISTS project_cache (
  id TEXT PRIMARY KEY,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  stack_id TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  service_count INTEGER NOT NULL DEFAULT 0,
  running_count INTEGER NOT NULL DEFAULT 0,
  auto_update BOOLEAN NOT NULL DEFAULT FALSE,
  last_modified TIMESTAMPTZ,
  compose_hash TEXT NOT NULL DEFAULT '',
  cached_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_cache_stack_id ON project_cache(stack_id);

-- Rename projects -> stacks
ALTER TABLE IF EXISTS projects RENAME TO stacks;
COMMIT;