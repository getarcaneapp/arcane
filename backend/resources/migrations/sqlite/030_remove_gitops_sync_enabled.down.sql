-- Re-add the enabled column to gitops_syncs table
-- SQLite doesn't support ADD COLUMN with constraints directly, so we need to recreate the table

-- Create new table with enabled column
CREATE TABLE gitops_syncs_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    branch TEXT NOT NULL,
    compose_path TEXT NOT NULL,
    project_name TEXT NOT NULL,
    project_id TEXT,
    auto_sync BOOLEAN NOT NULL DEFAULT false,
    sync_interval INTEGER NOT NULL DEFAULT 60,
    last_sync_at DATETIME,
    last_sync_status TEXT,
    last_sync_error TEXT,
    last_sync_commit TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE,
    FOREIGN KEY (repository_id) REFERENCES git_repositories(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);

-- Copy data from old table to new table
INSERT INTO gitops_syncs_new (
    id, name, environment_id, repository_id, branch, compose_path,
    project_name, project_id, auto_sync, sync_interval, last_sync_at,
    last_sync_status, last_sync_error, last_sync_commit, enabled, created_at, updated_at
)
SELECT 
    id, name, environment_id, repository_id, branch, compose_path,
    project_name, project_id, auto_sync, sync_interval, last_sync_at,
    last_sync_status, last_sync_error, last_sync_commit, true, created_at, updated_at
FROM gitops_syncs;

-- Drop old table
DROP TABLE gitops_syncs;

-- Rename new table to original name
ALTER TABLE gitops_syncs_new RENAME TO gitops_syncs;

-- Recreate indexes
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_environment_id ON gitops_syncs(environment_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_repository_id ON gitops_syncs(repository_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_project_id ON gitops_syncs(project_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_enabled ON gitops_syncs(enabled);
