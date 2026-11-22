package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v121SQLiteUp = `
UPDATE roles
SET permissions = json_insert(roles.permissions, '$[#]', 'projects:export')
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:create')
    AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:export');
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    source.api_key_id,
    'projects:export',
    source.environment_id,
    CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'projects:create'
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'projects:export'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v121PostgresUp = `
UPDATE roles SET permissions = permissions || '["projects:export"]'::jsonb
WHERE permissions @> '["projects:create"]'::jsonb AND NOT permissions @> '["projects:export"]'::jsonb;
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, source.api_key_id, 'projects:export', source.environment_id, CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'projects:create'
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'projects:export'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v121SQLiteDown = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value <> 'projects:export'
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:export');
DELETE FROM api_key_permissions WHERE permission = 'projects:export';
`
	v121PostgresDown = `
UPDATE roles SET permissions = permissions - 'projects:export' WHERE permissions @> '["projects:export"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission = 'projects:export';
`
)

func init() {
	goose.AddMigrationContext(run(v121SQLiteUp, v121PostgresUp), run(v121SQLiteDown, v121PostgresDown))
}
