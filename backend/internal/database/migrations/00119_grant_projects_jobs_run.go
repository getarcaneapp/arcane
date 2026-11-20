package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v119SQLiteUp = `
UPDATE roles
SET permissions = json_insert(roles.permissions, '$[#]', 'projects:jobs:run')
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:deploy')
    AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:jobs:run');
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    source.api_key_id,
    'projects:jobs:run',
    source.environment_id,
    CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'projects:deploy'
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'projects:jobs:run'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v119PostgresUp = `
UPDATE roles SET permissions = permissions || '["projects:jobs:run"]'::jsonb
WHERE permissions @> '["projects:deploy"]'::jsonb AND NOT permissions @> '["projects:jobs:run"]'::jsonb;
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, source.api_key_id, 'projects:jobs:run', source.environment_id, CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'projects:deploy'
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'projects:jobs:run'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v119SQLiteDown = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value <> 'projects:jobs:run'
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'projects:jobs:run');
DELETE FROM api_key_permissions WHERE permission = 'projects:jobs:run';
`
	v119PostgresDown = `
UPDATE roles SET permissions = permissions - 'projects:jobs:run' WHERE permissions @> '["projects:jobs:run"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission = 'projects:jobs:run';
`
)

func init() {
	goose.AddMigrationContext(run(v119SQLiteUp, v119PostgresUp), run(v119SQLiteDown, v119PostgresDown))
}
