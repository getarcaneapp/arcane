CREATE TABLE IF NOT EXISTS git_repositories (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    auth_type TEXT NOT NULL,
    username TEXT,
    token TEXT,
    ssh_key TEXT,
    description TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_git_repositories_enabled ON git_repositories(enabled);
CREATE INDEX IF NOT EXISTS idx_git_repositories_name ON git_repositories(name);

CREATE TABLE IF NOT EXISTS gitops_syncs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    branch TEXT NOT NULL,
    compose_path TEXT NOT NULL,
    project_name TEXT NOT NULL,
    project_id TEXT,
    auto_sync INTEGER NOT NULL DEFAULT 0,
    sync_interval INTEGER NOT NULL DEFAULT 60,
    last_sync_at DATETIME,
    last_sync_status TEXT,
    last_sync_error TEXT,
    last_sync_commit TEXT,
    environment_id TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (repository_id) REFERENCES git_repositories(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_gitops_syncs_environment_id ON gitops_syncs(environment_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_repository_id ON gitops_syncs(repository_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_project_id ON gitops_syncs(project_id);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_enabled ON gitops_syncs(enabled);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_auto_sync ON gitops_syncs(auto_sync);
CREATE INDEX IF NOT EXISTS idx_gitops_syncs_last_sync_commit ON gitops_syncs(last_sync_commit);

-- Add gitops_managed_by column to projects table
ALTER TABLE projects ADD COLUMN gitops_managed_by TEXT;
CREATE INDEX IF NOT EXISTS idx_projects_gitops_managed_by ON projects(gitops_managed_by);
