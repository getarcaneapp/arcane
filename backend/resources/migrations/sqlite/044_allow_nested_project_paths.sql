-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys=OFF;

BEGIN;

CREATE TABLE projects_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    dir_name TEXT,
    path TEXT NOT NULL,
    status TEXT NOT NULL,
    service_count INTEGER NOT NULL DEFAULT 0,
    running_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    status_reason TEXT,
    gitops_managed_by TEXT
);

INSERT INTO projects_new (
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
FROM projects;

DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;

CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
CREATE INDEX IF NOT EXISTS idx_projects_gitops_managed_by ON projects(gitops_managed_by);
CREATE INDEX IF NOT EXISTS idx_projects_dir_name_not_null ON projects(dir_name) WHERE dir_name IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_path_unique ON projects(path);

COMMIT;
PRAGMA foreign_keys=ON;

-- +goose Down
PRAGMA foreign_keys=OFF;

-- Rollback is only safe when dir_name values remain unique.
-- If nested projects introduced duplicate leaf directory names after the up migration,
-- recreating UNIQUE(dir_name) below will fail and the rollback must be handled manually.

BEGIN;

CREATE TABLE projects_new (
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

INSERT INTO projects_new (
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
FROM projects;

DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;

CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
CREATE INDEX IF NOT EXISTS idx_projects_gitops_managed_by ON projects(gitops_managed_by);
CREATE INDEX IF NOT EXISTS idx_projects_path ON projects(path);
CREATE INDEX IF NOT EXISTS idx_projects_dir_name_not_null ON projects(dir_name) WHERE dir_name IS NOT NULL;

COMMIT;
PRAGMA foreign_keys=ON;
