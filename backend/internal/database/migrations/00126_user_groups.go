package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v126SQLiteUp = `
CREATE TABLE IF NOT EXISTS groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS group_members (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'manual',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_gm_group ON group_members(group_id);
CREATE INDEX IF NOT EXISTS idx_gm_user ON group_members(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gm_uniq ON group_members(group_id, user_id, source);

CREATE TABLE IF NOT EXISTS group_role_assignments (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'manual',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_gra_group ON group_role_assignments(group_id);
CREATE INDEX IF NOT EXISTS idx_gra_role ON group_role_assignments(role_id);
CREATE INDEX IF NOT EXISTS idx_gra_env ON group_role_assignments(environment_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gra_uniq ON group_role_assignments(group_id, role_id, COALESCE(environment_id, ''), source);

UPDATE roles
SET permissions = json_insert(roles.permissions, '$[#]', 'groups:read')
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'roles:read')
    AND EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'users:read')
    AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'groups:read');
UPDATE roles
SET permissions = json_insert(roles.permissions, '$[#]', 'groups:list')
WHERE (
        (EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'roles:list')
            AND EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'users:list'))
        OR EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'groups:read')
    )
    AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'groups:list');

INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    source.api_key_id,
    'groups:list',
    source.environment_id,
    CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'roles:list'
    AND EXISTS (
        SELECT 1 FROM api_key_permissions AS users_perm
        WHERE users_perm.api_key_id = source.api_key_id
            AND users_perm.permission = 'users:list'
            AND COALESCE(users_perm.environment_id, '') = COALESCE(source.environment_id, '')
    )
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'groups:list'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );

INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    source.api_key_id,
    'groups:read',
    source.environment_id,
    CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'roles:read'
    AND EXISTS (
        SELECT 1 FROM api_key_permissions AS users_perm
        WHERE users_perm.api_key_id = source.api_key_id
            AND users_perm.permission = 'users:read'
            AND COALESCE(users_perm.environment_id, '') = COALESCE(source.environment_id, '')
    )
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'groups:read'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v126PostgresUp = `
CREATE TABLE IF NOT EXISTS groups (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    description text,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone
);

CREATE TABLE IF NOT EXISTS group_members (
    id text PRIMARY KEY,
    group_id text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source text NOT NULL DEFAULT 'manual',
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS idx_gm_group ON group_members USING btree (group_id);
CREATE INDEX IF NOT EXISTS idx_gm_user ON group_members USING btree (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gm_uniq ON group_members USING btree (group_id, user_id, source);

CREATE TABLE IF NOT EXISTS group_role_assignments (
    id text PRIMARY KEY,
    group_id text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role_id text NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id text REFERENCES environments(id) ON DELETE CASCADE,
    source text NOT NULL DEFAULT 'manual',
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS idx_gra_group ON group_role_assignments USING btree (group_id);
CREATE INDEX IF NOT EXISTS idx_gra_role ON group_role_assignments USING btree (role_id);
CREATE INDEX IF NOT EXISTS idx_gra_env ON group_role_assignments USING btree (environment_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gra_uniq ON group_role_assignments USING btree (group_id, role_id, COALESCE(environment_id, ''::text), source);

UPDATE roles SET permissions = permissions || '["groups:read"]'::jsonb
WHERE permissions @> '["roles:read", "users:read"]'::jsonb
    AND NOT permissions @> '["groups:read"]'::jsonb;
UPDATE roles SET permissions = permissions || '["groups:list"]'::jsonb
WHERE (
        permissions @> '["roles:list", "users:list"]'::jsonb
        OR permissions @> '["groups:read"]'::jsonb
    )
    AND NOT permissions @> '["groups:list"]'::jsonb;

INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, source.api_key_id, 'groups:list', source.environment_id, CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'roles:list'
    AND EXISTS (
        SELECT 1 FROM api_key_permissions AS users_perm
        WHERE users_perm.api_key_id = source.api_key_id
            AND users_perm.permission = 'users:list'
            AND COALESCE(users_perm.environment_id, '') = COALESCE(source.environment_id, '')
    )
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'groups:list'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );

INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, source.api_key_id, 'groups:read', source.environment_id, CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'roles:read'
    AND EXISTS (
        SELECT 1 FROM api_key_permissions AS users_perm
        WHERE users_perm.api_key_id = source.api_key_id
            AND users_perm.permission = 'users:read'
            AND COALESCE(users_perm.environment_id, '') = COALESCE(source.environment_id, '')
    )
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'groups:read'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v126SQLiteDown = `
UPDATE roles
SET permissions = (
    SELECT COALESCE(json_group_array(value), '[]') FROM json_each(roles.permissions) WHERE value NOT IN ('groups:list', 'groups:read')
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value IN ('groups:list', 'groups:read'));
DELETE FROM api_key_permissions WHERE permission IN ('groups:list', 'groups:read');
DROP TABLE IF EXISTS group_role_assignments;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
`
	v126PostgresDown = `
UPDATE roles SET permissions = permissions - 'groups:list' - 'groups:read'
WHERE permissions @> '["groups:list"]'::jsonb OR permissions @> '["groups:read"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission IN ('groups:list', 'groups:read');
DROP TABLE IF EXISTS group_role_assignments;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
`
)

func init() {
	goose.AddMigrationContext(run(v126SQLiteUp, v126PostgresUp), run(v126SQLiteDown, v126PostgresDown))
}
