package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v115SQLiteUp = `
UPDATE roles
SET permissions = json_insert(roles.permissions, '$[#]', 'gitops:bidirectional')
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'gitops:backup')
    AND NOT EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'gitops:bidirectional');
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
    source.api_key_id,
    'gitops:bidirectional',
    source.environment_id,
    CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'gitops:backup'
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'gitops:bidirectional'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v115PostgresUp = `
UPDATE roles SET permissions = permissions || '["gitops:bidirectional"]'::jsonb
WHERE permissions @> '["gitops:backup"]'::jsonb AND NOT permissions @> '["gitops:bidirectional"]'::jsonb;
INSERT INTO api_key_permissions (id, api_key_id, permission, environment_id, created_at)
SELECT gen_random_uuid()::text, source.api_key_id, 'gitops:bidirectional', source.environment_id, CURRENT_TIMESTAMP
FROM api_key_permissions AS source
WHERE source.permission = 'gitops:backup'
    AND NOT EXISTS (
        SELECT 1 FROM api_key_permissions AS existing
        WHERE existing.api_key_id = source.api_key_id
            AND existing.permission = 'gitops:bidirectional'
            AND COALESCE(existing.environment_id, '') = COALESCE(source.environment_id, '')
    );
`
	v115SQLiteDown = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value <> 'gitops:bidirectional'
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'gitops:bidirectional');
DELETE FROM api_key_permissions WHERE permission = 'gitops:bidirectional';
`
	v115PostgresDown = `
UPDATE roles SET permissions = permissions - 'gitops:bidirectional' WHERE permissions @> '["gitops:bidirectional"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission = 'gitops:bidirectional';
`
)

func init() {
	goose.AddMigrationContext(run(v115SQLiteUp, v115PostgresUp), run(v115SQLiteDown, v115PostgresDown))
}
