PRAGMA foreign_keys=OFF;

-- Rollback is only safe when dir_name values remain unique.
-- If nested projects introduced duplicate leaf directory names after the up migration,
-- recreating UNIQUE(dir_name) below will fail and the rollback must be handled manually.

ALTER TABLE projects RENAME TO projects_old;

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    dir_name TEXT UNIQUE,
    path TEXT NOT NULL,
    status TEXT NOT NULL,
    service_count INTEGER NOT NULL DEFAULT 0,
    running_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    status_reason TEXT,
    gitops_managed_by TEXT
);

INSERT INTO projects (
    id,
    name,
    dir_name,
    path,
    status,
    service_count,
    running_count,
    created_at,
    updated_at,
    status_reason,
    gitops_managed_by
)
SELECT
    id,
    name,
    dir_name,
    path,
    status,
    service_count,
    running_count,
    created_at,
    updated_at,
    status_reason,
    gitops_managed_by
FROM projects_old;

DROP TABLE projects_old;

CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
CREATE INDEX IF NOT EXISTS idx_projects_gitops_managed_by ON projects(gitops_managed_by);
CREATE INDEX IF NOT EXISTS idx_projects_path ON projects(path);
CREATE INDEX IF NOT EXISTS idx_projects_dir_name_not_null ON projects(dir_name) WHERE dir_name IS NOT NULL;

PRAGMA foreign_keys=ON;
