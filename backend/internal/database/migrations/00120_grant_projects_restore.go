package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v120SQLiteUp = `
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
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value <> 'projects:restore'
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:restore');
DELETE FROM api_key_permissions WHERE permission = 'projects:restore';
`
	v120PostgresDown = `
UPDATE roles SET permissions = permissions - 'projects:restore' WHERE permissions @> '["projects:restore"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission = 'projects:restore';
`
)

func init() {
	goose.AddMigrationContext(run(v120SQLiteUp, v120PostgresUp), run(v120SQLiteDown, v120PostgresDown))
}
