package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v117SQLiteUp = `
CREATE TABLE IF NOT EXISTS secret_sources (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    settings TEXT NOT NULL DEFAULT '{}',
    credential TEXT NOT NULL DEFAULT '',
    last_tested_at DATETIME,
    last_test_error TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_secret_sources_name ON secret_sources(name);

-- A binding connects a project to one target of a source; a project can have
-- several, applied in position order. owner_kind leaves room for standalone
-- containers (container_name) later; only projects are bound today.
CREATE TABLE IF NOT EXISTS secret_bindings (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
    owner_kind TEXT NOT NULL DEFAULT 'project',
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    container_name TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    source_id TEXT NOT NULL REFERENCES secret_sources(id) ON DELETE RESTRICT,
    target TEXT NOT NULL DEFAULT '{}',
    required BOOLEAN NOT NULL DEFAULT TRUE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    auto_redeploy BOOLEAN NOT NULL DEFAULT FALSE,
    hash_salt TEXT NOT NULL,
    deployed_hash TEXT,
    deployed_keys TEXT,
    deployed_at DATETIME,
    last_seen_hash TEXT,
    last_notified_hash TEXT,
    last_checked_at DATETIME,
    last_fetched_at DATETIME,
    last_fetch_error TEXT,
    CHECK (
        (owner_kind = 'project' AND project_id IS NOT NULL)
        OR (owner_kind = 'container' AND container_name IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_secret_bindings_project ON secret_bindings(project_id, position);
CREATE INDEX IF NOT EXISTS idx_secret_bindings_source ON secret_bindings(source_id);
`
	v117SQLiteDown = `
DROP TABLE IF EXISTS secret_bindings;
DROP TABLE IF EXISTS secret_sources;
`
	v117PostgresUp = `
CREATE TABLE IF NOT EXISTS secret_sources (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    settings TEXT NOT NULL DEFAULT '{}',
    credential TEXT NOT NULL DEFAULT '',
    last_tested_at TIMESTAMPTZ,
    last_test_error TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_secret_sources_name ON secret_sources(name);

-- A binding connects a project to one target of a source; a project can have
-- several, applied in position order. owner_kind leaves room for standalone
-- containers (container_name) later; only projects are bound today.
CREATE TABLE IF NOT EXISTS secret_bindings (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ,
    owner_kind TEXT NOT NULL DEFAULT 'project',
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    container_name TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    source_id TEXT NOT NULL REFERENCES secret_sources(id) ON DELETE RESTRICT,
    target TEXT NOT NULL DEFAULT '{}',
    required BOOLEAN NOT NULL DEFAULT TRUE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    auto_redeploy BOOLEAN NOT NULL DEFAULT FALSE,
    hash_salt TEXT NOT NULL,
    deployed_hash TEXT,
    deployed_keys TEXT,
    deployed_at TIMESTAMPTZ,
    last_seen_hash TEXT,
    last_notified_hash TEXT,
    last_checked_at TIMESTAMPTZ,
    last_fetched_at TIMESTAMPTZ,
    last_fetch_error TEXT,
    CHECK (
        (owner_kind = 'project' AND project_id IS NOT NULL)
        OR (owner_kind = 'container' AND container_name IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_secret_bindings_project ON secret_bindings(project_id, position);
CREATE INDEX IF NOT EXISTS idx_secret_bindings_source ON secret_bindings(source_id);
`
	v117PostgresDown = `
DROP TABLE IF EXISTS secret_bindings;
DROP TABLE IF EXISTS secret_sources;
`
)

func init() {
	goose.AddMigrationContext(run(v117SQLiteUp, v117PostgresUp), run(v117SQLiteDown, v117PostgresDown))
}
