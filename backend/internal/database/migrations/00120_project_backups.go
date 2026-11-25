package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v120SQLiteUp = `
CREATE TABLE IF NOT EXISTS project_backups (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    project_name TEXT NOT NULL,
    compose_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'running',
    trigger TEXT NOT NULL DEFAULT 'manual',
    destination TEXT NOT NULL DEFAULT 'local',
    local_snapshot_id TEXT NOT NULL DEFAULT '',
    remote_snapshot_id TEXT NOT NULL DEFAULT '',
    s3_destination_id TEXT NOT NULL DEFAULT '',
    size INTEGER NOT NULL DEFAULT 0,
    volumes TEXT DEFAULT '[]',
    stopped_containers BOOLEAN NOT NULL DEFAULT false,
    files BOOLEAN NOT NULL DEFAULT true,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP,
    completed_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_project_backups_project_id ON project_backups(project_id);
UPDATE roles
SET permissions = json_insert(roles.permissions, '$[#]', 'projects:backup')
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'volumes:backup')
    AND EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:read')
    AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:backup');
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    grants.api_key_id,
    'projects:backup',
    grants.environment_id,
    CURRENT_TIMESTAMP
FROM (
    SELECT DISTINCT
        source.api_key_id AS api_key_id,
        CASE WHEN COALESCE(source.environment_id, '') = '' THEN reader.environment_id ELSE source.environment_id END AS environment_id
    FROM api_key_permissions AS source
    JOIN api_key_permissions AS reader
        ON reader.api_key_id = source.api_key_id AND reader.permission = 'projects:read'
    WHERE source.permission = 'volumes:backup'
        AND (COALESCE(source.environment_id, '') = '' OR COALESCE(reader.environment_id, '') = ''
            OR reader.environment_id = source.environment_id)
) AS grants
WHERE NOT EXISTS (
    SELECT 1 FROM api_key_permissions AS existing
    WHERE existing.api_key_id = grants.api_key_id
        AND existing.permission = 'projects:backup'
        AND COALESCE(existing.environment_id, '') = COALESCE(grants.environment_id, '')
);
UPDATE roles
SET permissions = json_insert(roles.permissions, '$[#]', 'projects:restore')
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'volumes:backup')
    AND EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:read')
    AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:restore');
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    grants.api_key_id,
    'projects:restore',
    grants.environment_id,
    CURRENT_TIMESTAMP
FROM (
    SELECT DISTINCT
        source.api_key_id AS api_key_id,
        CASE WHEN COALESCE(source.environment_id, '') = '' THEN reader.environment_id ELSE source.environment_id END AS environment_id
    FROM api_key_permissions AS source
    JOIN api_key_permissions AS reader
        ON reader.api_key_id = source.api_key_id AND reader.permission = 'projects:read'
    WHERE source.permission = 'volumes:backup'
        AND (COALESCE(source.environment_id, '') = '' OR COALESCE(reader.environment_id, '') = ''
            OR reader.environment_id = source.environment_id)
) AS grants
WHERE NOT EXISTS (
    SELECT 1 FROM api_key_permissions AS existing
    WHERE existing.api_key_id = grants.api_key_id
        AND existing.permission = 'projects:restore'
        AND COALESCE(existing.environment_id, '') = COALESCE(grants.environment_id, '')
);
`
	v120PostgresUp = `
CREATE TABLE IF NOT EXISTS project_backups (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    project_name text NOT NULL,
    compose_name text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'running',
    trigger text NOT NULL DEFAULT 'manual',
    destination text NOT NULL DEFAULT 'local',
    local_snapshot_id text NOT NULL DEFAULT '',
    remote_snapshot_id text NOT NULL DEFAULT '',
    s3_destination_id text NOT NULL DEFAULT '',
    size bigint NOT NULL DEFAULT 0,
    volumes text DEFAULT '[]',
    stopped_containers boolean NOT NULL DEFAULT false,
    files boolean NOT NULL DEFAULT true,
    error text NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone,
    completed_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS idx_project_backups_project_id ON project_backups(project_id);
UPDATE roles SET permissions = permissions || '["projects:backup"]'::jsonb
WHERE permissions @> '["volumes:backup"]'::jsonb AND permissions @> '["projects:read"]'::jsonb
    AND NOT permissions @> '["projects:backup"]'::jsonb;
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, grants.api_key_id, 'projects:backup', grants.environment_id, CURRENT_TIMESTAMP
FROM (
    SELECT DISTINCT
        source.api_key_id AS api_key_id,
        CASE WHEN COALESCE(source.environment_id, '') = '' THEN reader.environment_id ELSE source.environment_id END AS environment_id
    FROM api_key_permissions AS source
    JOIN api_key_permissions AS reader
        ON reader.api_key_id = source.api_key_id AND reader.permission = 'projects:read'
    WHERE source.permission = 'volumes:backup'
        AND (COALESCE(source.environment_id, '') = '' OR COALESCE(reader.environment_id, '') = ''
            OR reader.environment_id = source.environment_id)
) AS grants
WHERE NOT EXISTS (
    SELECT 1 FROM api_key_permissions AS existing
    WHERE existing.api_key_id = grants.api_key_id
        AND existing.permission = 'projects:backup'
        AND COALESCE(existing.environment_id, '') = COALESCE(grants.environment_id, '')
);
UPDATE roles SET permissions = permissions || '["projects:restore"]'::jsonb
WHERE permissions @> '["volumes:backup"]'::jsonb AND permissions @> '["projects:read"]'::jsonb
    AND NOT permissions @> '["projects:restore"]'::jsonb;
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, grants.api_key_id, 'projects:restore', grants.environment_id, CURRENT_TIMESTAMP
FROM (
    SELECT DISTINCT
        source.api_key_id AS api_key_id,
        CASE WHEN COALESCE(source.environment_id, '') = '' THEN reader.environment_id ELSE source.environment_id END AS environment_id
    FROM api_key_permissions AS source
    JOIN api_key_permissions AS reader
        ON reader.api_key_id = source.api_key_id AND reader.permission = 'projects:read'
    WHERE source.permission = 'volumes:backup'
        AND (COALESCE(source.environment_id, '') = '' OR COALESCE(reader.environment_id, '') = ''
            OR reader.environment_id = source.environment_id)
) AS grants
WHERE NOT EXISTS (
    SELECT 1 FROM api_key_permissions AS existing
    WHERE existing.api_key_id = grants.api_key_id
        AND existing.permission = 'projects:restore'
        AND COALESCE(existing.environment_id, '') = COALESCE(grants.environment_id, '')
);
`
	v120SQLiteDown = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value NOT IN ('projects:backup', 'projects:restore')
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value IN ('projects:backup', 'projects:restore'));
DELETE FROM api_key_permissions WHERE permission IN ('projects:backup', 'projects:restore');
DROP INDEX IF EXISTS idx_project_backups_project_id;
DROP TABLE IF EXISTS project_backups;
`
	v120PostgresDown = `
UPDATE roles SET permissions = permissions - 'projects:backup' - 'projects:restore'
WHERE permissions @> '["projects:backup"]'::jsonb OR permissions @> '["projects:restore"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission IN ('projects:backup', 'projects:restore');
DROP INDEX IF EXISTS idx_project_backups_project_id;
DROP TABLE IF EXISTS project_backups;
`
)

func init() {
	goose.AddMigrationContext(run(v120SQLiteUp, v120PostgresUp), run(v120SQLiteDown, v120PostgresDown))
}
