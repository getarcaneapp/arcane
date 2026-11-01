package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v100SQLiteUp = `
UPDATE roles
SET permissions = (
    SELECT json_group_array(value) FROM json_each(roles.permissions) WHERE value <> 'dashboard:read'
)
WHERE EXISTS (SELECT 1 FROM json_each(roles.permissions) WHERE value = 'dashboard:read');
DELETE FROM api_key_permissions WHERE permission = 'dashboard:read';
`
	v100PostgresUp = `
UPDATE roles SET permissions = permissions - 'dashboard:read' WHERE permissions @> '["dashboard:read"]'::jsonb;
DELETE FROM api_key_permissions WHERE permission = 'dashboard:read';
`
)

func init() {
	goose.AddMigrationContext(run(v100SQLiteUp, v100PostgresUp), nil)
}
