package migrations

import (
	"github.com/pressly/goose/v3"
)

// Migration 100 replaces role assignments with role bindings and groups and adds audit events.
func init() {
	goose.AddMigrationContext(run(v100SQLiteUp, v100PostgresUp), run(v100SQLiteDown, v100PostgresDown))
}

const (
	// v100SQLiteUUID generates a random UUIDv4 in SQLite.
	v100SQLiteUUID = `lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || ` +
		`substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))`

	v100SQLiteUp = `
UPDATE roles SET permissions = '[]' WHERE built_in = 1;
UPDATE roles SET permissions = (SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value <> 'images:push')
WHERE built_in = 0 AND EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'images:push');

INSERT INTO roles (id, name, description, permissions, built_in, created_at) VALUES
    ('role_admin', 'Admin', 'Full administrative access', '[]', 1, CURRENT_TIMESTAMP),
    ('role_editor', 'Editor', 'Read and write on Docker resources', '[]', 1, CURRENT_TIMESTAMP),
    ('role_no_shell_editor', 'No-Shell Editor', 'Editor without interactive container shell access', '[]', 1, CURRENT_TIMESTAMP),
    ('role_deployer', 'Deployer', 'Deploy and lifecycle containers and projects', '[]', 1, CURRENT_TIMESTAMP),
    ('role_monitor', 'Monitor', 'Observability-only access: logs, dashboards, events', '[]', 1, CURRENT_TIMESTAMP),
    ('role_viewer', 'Viewer', 'Read-only access to all resources', '[]', 1, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS groups (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME
);

CREATE TABLE IF NOT EXISTS group_members (
    id         TEXT PRIMARY KEY,
    group_id   TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source     TEXT NOT NULL DEFAULT 'manual',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_group_members_uniq ON group_members(group_id, user_id, source);
CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);

CREATE TABLE IF NOT EXISTS oidc_group_mappings (
    id          TEXT PRIMARY KEY,
    claim_value TEXT NOT NULL,
    group_id    TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    source      TEXT NOT NULL DEFAULT 'manual',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oidc_group_mappings_uniq ON oidc_group_mappings(claim_value, group_id);

CREATE TABLE IF NOT EXISTS role_bindings (
    id             TEXT PRIMARY KEY,
    user_id        TEXT REFERENCES users(id) ON DELETE CASCADE,
    group_id       TEXT REFERENCES groups(id) ON DELETE CASCADE,
    role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'manual',
    created_by     TEXT,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME,
    CHECK ((user_id IS NULL) <> (group_id IS NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_role_bindings_uniq
    ON role_bindings(COALESCE(user_id, ''), COALESCE(group_id, ''), role_id, COALESCE(environment_id, ''), source);
CREATE INDEX IF NOT EXISTS idx_role_bindings_user ON role_bindings(user_id);
CREATE INDEX IF NOT EXISTS idx_role_bindings_group ON role_bindings(group_id);
CREATE INDEX IF NOT EXISTS idx_role_bindings_role ON role_bindings(role_id);

INSERT INTO role_bindings (id, user_id, role_id, environment_id, source, created_at, updated_at)
SELECT ura.id, ura.user_id, ura.role_id, ura.environment_id,
       CASE
           WHEN ura.source = 'oidc' THEN 'oidc'
           WHEN EXISTS (SELECT 1 FROM federated_credentials fc WHERE fc.identity_user_id = ura.user_id) THEN 'federated'
           ELSE 'manual'
       END,
       ura.created_at, ura.updated_at
FROM user_role_assignments ura;

INSERT INTO role_bindings (id, user_id, role_id, source, created_at)
SELECT ` + v100SQLiteUUID + `, u.id,
       CASE WHEN EXISTS (
           SELECT 1 FROM json_each(COALESCE(CASE WHEN json_valid(u.roles) THEN CASE WHEN json_type(u.roles) = 'array' THEN u.roles END END, '[]')) r
           WHERE lower(trim(r.value)) = 'admin'
       ) THEN 'role_admin' ELSE 'role_viewer' END,
       'manual', CURRENT_TIMESTAMP
FROM users u
WHERE NOT EXISTS (SELECT 1 FROM kv WHERE "key" = 'migration.legacy_user_roles.v1.completed')
  AND NOT EXISTS (SELECT 1 FROM user_role_assignments ura WHERE ura.user_id = u.id);

INSERT INTO groups (id, name, created_at)
SELECT ` + v100SQLiteUUID + `, claims.claim_value, CURRENT_TIMESTAMP
FROM (SELECT DISTINCT claim_value FROM oidc_role_mappings) claims;

INSERT INTO role_bindings (id, group_id, role_id, environment_id, source, created_at)
SELECT ` + v100SQLiteUUID + `, pairs.group_id, pairs.role_id, pairs.environment_id, 'manual', CURRENT_TIMESTAMP
FROM (
    SELECT DISTINCT g.id AS group_id, m.role_id, m.environment_id
    FROM oidc_role_mappings m
    INNER JOIN groups g ON g.name = m.claim_value
) pairs;

INSERT INTO oidc_group_mappings (id, claim_value, group_id, source, created_at)
SELECT ` + v100SQLiteUUID + `, sources.claim_value, g.id, sources.source, CURRENT_TIMESTAMP
FROM (SELECT claim_value, MIN(source) AS source FROM oidc_role_mappings GROUP BY claim_value) sources
INNER JOIN groups g ON g.name = sources.claim_value;

DROP TABLE IF EXISTS user_role_assignments;
DROP TABLE IF EXISTS oidc_role_mappings;

ALTER TABLE events ADD COLUMN audit BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_events_audit_timestamp ON events(audit, timestamp);
`

	v100SQLiteDown = `
DELETE FROM events WHERE audit = true;
DROP INDEX IF EXISTS idx_events_audit_timestamp;
ALTER TABLE events DROP COLUMN audit;

CREATE TABLE IF NOT EXISTS user_role_assignments (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'manual',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME
);

CREATE INDEX IF NOT EXISTS idx_ura_user ON user_role_assignments(user_id);
CREATE INDEX IF NOT EXISTS idx_ura_role ON user_role_assignments(role_id);
CREATE INDEX IF NOT EXISTS idx_ura_env ON user_role_assignments(environment_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ura_uniq
    ON user_role_assignments(user_id, role_id, COALESCE(environment_id, ''), source);

CREATE TABLE IF NOT EXISTS oidc_role_mappings (
    id             TEXT PRIMARY KEY,
    claim_value    TEXT NOT NULL,
    role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'manual',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME
);

CREATE INDEX IF NOT EXISTS idx_orm_claim ON oidc_role_mappings(claim_value);

INSERT INTO user_role_assignments (id, user_id, role_id, environment_id, source, created_at, updated_at)
SELECT id, user_id, role_id, environment_id, CASE WHEN source = 'oidc' THEN 'oidc' ELSE 'manual' END, created_at, updated_at
FROM role_bindings
WHERE user_id IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO user_role_assignments (id, user_id, role_id, environment_id, source, created_at)
SELECT ` + v100SQLiteUUID + `, gm.user_id, rb.role_id, rb.environment_id, 'oidc', CURRENT_TIMESTAMP
FROM group_members gm
INNER JOIN role_bindings rb ON rb.group_id = gm.group_id
WHERE gm.source = 'oidc'
ON CONFLICT DO NOTHING;

INSERT INTO oidc_role_mappings (id, claim_value, role_id, environment_id, source, created_at)
SELECT ` + v100SQLiteUUID + `, m.claim_value, rb.role_id, rb.environment_id, m.source, CURRENT_TIMESTAMP
FROM oidc_group_mappings m
INNER JOIN role_bindings rb ON rb.group_id = m.group_id;

DROP INDEX IF EXISTS idx_role_bindings_role;
DROP INDEX IF EXISTS idx_role_bindings_group;
DROP INDEX IF EXISTS idx_role_bindings_user;
DROP INDEX IF EXISTS idx_role_bindings_uniq;
DROP TABLE IF EXISTS role_bindings;

DROP INDEX IF EXISTS idx_oidc_group_mappings_uniq;
DROP TABLE IF EXISTS oidc_group_mappings;

DROP INDEX IF EXISTS idx_group_members_user;
DROP INDEX IF EXISTS idx_group_members_uniq;
DROP TABLE IF EXISTS group_members;

DROP TABLE IF EXISTS groups;
`

	v100PostgresUp = `
UPDATE roles SET permissions = '[]'::jsonb WHERE built_in = true;
UPDATE roles SET permissions = permissions - 'images:push' WHERE built_in = false AND permissions ? 'images:push';

INSERT INTO roles (id, name, description, permissions, built_in, created_at) VALUES
    ('role_admin', 'Admin', 'Full administrative access', '[]'::jsonb, true, CURRENT_TIMESTAMP),
    ('role_editor', 'Editor', 'Read and write on Docker resources', '[]'::jsonb, true, CURRENT_TIMESTAMP),
    ('role_no_shell_editor', 'No-Shell Editor', 'Editor without interactive container shell access', '[]'::jsonb, true, CURRENT_TIMESTAMP),
    ('role_deployer', 'Deployer', 'Deploy and lifecycle containers and projects', '[]'::jsonb, true, CURRENT_TIMESTAMP),
    ('role_monitor', 'Monitor', 'Observability-only access: logs, dashboards, events', '[]'::jsonb, true, CURRENT_TIMESTAMP),
    ('role_viewer', 'Viewer', 'Read-only access to all resources', '[]'::jsonb, true, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS groups (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS group_members (
    id         TEXT PRIMARY KEY,
    group_id   TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source     TEXT NOT NULL DEFAULT 'manual',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_group_members_uniq ON group_members(group_id, user_id, source);
CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);

CREATE TABLE IF NOT EXISTS oidc_group_mappings (
    id          TEXT PRIMARY KEY,
    claim_value TEXT NOT NULL,
    group_id    TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    source      TEXT NOT NULL DEFAULT 'manual',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oidc_group_mappings_uniq ON oidc_group_mappings(claim_value, group_id);

CREATE TABLE IF NOT EXISTS role_bindings (
    id             TEXT PRIMARY KEY,
    user_id        TEXT REFERENCES users(id) ON DELETE CASCADE,
    group_id       TEXT REFERENCES groups(id) ON DELETE CASCADE,
    role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'manual',
    created_by     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ,
    CHECK ((user_id IS NULL) <> (group_id IS NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_role_bindings_uniq
    ON role_bindings(COALESCE(user_id, ''), COALESCE(group_id, ''), role_id, COALESCE(environment_id, ''), source);
CREATE INDEX IF NOT EXISTS idx_role_bindings_user ON role_bindings(user_id);
CREATE INDEX IF NOT EXISTS idx_role_bindings_group ON role_bindings(group_id);
CREATE INDEX IF NOT EXISTS idx_role_bindings_role ON role_bindings(role_id);

INSERT INTO role_bindings (id, user_id, role_id, environment_id, source, created_at, updated_at)
SELECT ura.id, ura.user_id, ura.role_id, ura.environment_id,
       CASE
           WHEN ura.source = 'oidc' THEN 'oidc'
           WHEN EXISTS (SELECT 1 FROM federated_credentials fc WHERE fc.identity_user_id = ura.user_id) THEN 'federated'
           ELSE 'manual'
       END,
       ura.created_at, ura.updated_at
FROM user_role_assignments ura;

INSERT INTO role_bindings (id, user_id, role_id, source, created_at)
SELECT gen_random_uuid()::text, u.id,
       CASE WHEN EXISTS (
           SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(u.roles) = 'array' THEN u.roles ELSE '[]'::jsonb END) r(value)
           WHERE lower(trim(r.value)) = 'admin'
       ) THEN 'role_admin' ELSE 'role_viewer' END,
       'manual', CURRENT_TIMESTAMP
FROM users u
WHERE NOT EXISTS (SELECT 1 FROM kv WHERE "key" = 'migration.legacy_user_roles.v1.completed')
  AND NOT EXISTS (SELECT 1 FROM user_role_assignments ura WHERE ura.user_id = u.id);

INSERT INTO groups (id, name, created_at)
SELECT gen_random_uuid()::text, claims.claim_value, CURRENT_TIMESTAMP
FROM (SELECT DISTINCT claim_value FROM oidc_role_mappings) claims;

INSERT INTO role_bindings (id, group_id, role_id, environment_id, source, created_at)
SELECT gen_random_uuid()::text, pairs.group_id, pairs.role_id, pairs.environment_id, 'manual', CURRENT_TIMESTAMP
FROM (
    SELECT DISTINCT g.id AS group_id, m.role_id, m.environment_id
    FROM oidc_role_mappings m
    INNER JOIN groups g ON g.name = m.claim_value
) pairs;

INSERT INTO oidc_group_mappings (id, claim_value, group_id, source, created_at)
SELECT gen_random_uuid()::text, sources.claim_value, g.id, sources.source, CURRENT_TIMESTAMP
FROM (SELECT claim_value, MIN(source) AS source FROM oidc_role_mappings GROUP BY claim_value) sources
INNER JOIN groups g ON g.name = sources.claim_value;

DROP TABLE IF EXISTS user_role_assignments;
DROP TABLE IF EXISTS oidc_role_mappings;

ALTER TABLE events ADD COLUMN IF NOT EXISTS audit BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_events_audit_timestamp ON events(audit, timestamp);
`

	v100PostgresDown = `
DELETE FROM events WHERE audit = true;
DROP INDEX IF EXISTS idx_events_audit_timestamp;
ALTER TABLE events DROP COLUMN IF EXISTS audit;

CREATE TABLE IF NOT EXISTS user_role_assignments (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'manual',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ura_user ON user_role_assignments(user_id);
CREATE INDEX IF NOT EXISTS idx_ura_role ON user_role_assignments(role_id);
CREATE INDEX IF NOT EXISTS idx_ura_env ON user_role_assignments(environment_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ura_uniq
    ON user_role_assignments(user_id, role_id, COALESCE(environment_id, ''), source);

CREATE TABLE IF NOT EXISTS oidc_role_mappings (
    id             TEXT PRIMARY KEY,
    claim_value    TEXT NOT NULL,
    role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'manual',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_orm_claim ON oidc_role_mappings(claim_value);

INSERT INTO user_role_assignments (id, user_id, role_id, environment_id, source, created_at, updated_at)
SELECT id, user_id, role_id, environment_id, CASE WHEN source = 'oidc' THEN 'oidc' ELSE 'manual' END, created_at, updated_at
FROM role_bindings
WHERE user_id IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO user_role_assignments (id, user_id, role_id, environment_id, source, created_at)
SELECT gen_random_uuid()::text, gm.user_id, rb.role_id, rb.environment_id, 'oidc', CURRENT_TIMESTAMP
FROM group_members gm
INNER JOIN role_bindings rb ON rb.group_id = gm.group_id
WHERE gm.source = 'oidc'
ON CONFLICT DO NOTHING;

INSERT INTO oidc_role_mappings (id, claim_value, role_id, environment_id, source, created_at)
SELECT gen_random_uuid()::text, m.claim_value, rb.role_id, rb.environment_id, m.source, CURRENT_TIMESTAMP
FROM oidc_group_mappings m
INNER JOIN role_bindings rb ON rb.group_id = m.group_id;

DROP INDEX IF EXISTS idx_role_bindings_role;
DROP INDEX IF EXISTS idx_role_bindings_group;
DROP INDEX IF EXISTS idx_role_bindings_user;
DROP INDEX IF EXISTS idx_role_bindings_uniq;
DROP TABLE IF EXISTS role_bindings;

DROP INDEX IF EXISTS idx_oidc_group_mappings_uniq;
DROP TABLE IF EXISTS oidc_group_mappings;

DROP INDEX IF EXISTS idx_group_members_user;
DROP INDEX IF EXISTS idx_group_members_uniq;
DROP TABLE IF EXISTS group_members;

DROP TABLE IF EXISTS groups;
`
)
