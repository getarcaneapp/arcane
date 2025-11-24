CREATE TABLE IF NOT EXISTS gitops_repositories (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    branch TEXT NOT NULL DEFAULT 'main',
    username TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL DEFAULT '',
    compose_path TEXT NOT NULL,
    project_name TEXT,
    description TEXT,
    auto_sync BOOLEAN NOT NULL DEFAULT false,
    sync_interval INTEGER NOT NULL DEFAULT 60,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_synced_at TIMESTAMP,
    last_sync_status TEXT,
    last_sync_error TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_gitops_repositories_enabled ON gitops_repositories(enabled);
CREATE INDEX IF NOT EXISTS idx_gitops_repositories_auto_sync ON gitops_repositories(auto_sync);
CREATE INDEX IF NOT EXISTS idx_gitops_repositories_created_at ON gitops_repositories(created_at);
CREATE INDEX IF NOT EXISTS idx_gitops_repositories_project_name ON gitops_repositories(project_name);
