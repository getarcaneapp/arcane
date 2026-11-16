package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v116SQLiteUp = `
ALTER TABLE users ADD COLUMN ldap_id TEXT;
ALTER TABLE users ADD COLUMN disabled BOOLEAN NOT NULL DEFAULT false;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ldap_id ON users(ldap_id);
`
	v116SQLiteDown = `
DROP INDEX IF EXISTS idx_users_ldap_id;
ALTER TABLE users DROP COLUMN disabled;
ALTER TABLE users DROP COLUMN ldap_id;
`
	v116PostgresUp = `
ALTER TABLE users ADD COLUMN IF NOT EXISTS ldap_id text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS disabled boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ldap_id ON users(ldap_id);
`
	v116PostgresDown = `
DROP INDEX IF EXISTS idx_users_ldap_id;
ALTER TABLE users DROP COLUMN IF EXISTS disabled;
ALTER TABLE users DROP COLUMN IF EXISTS ldap_id;
`
)

func init() {
	goose.AddMigrationContext(run(v116SQLiteUp, v116PostgresUp), run(v116SQLiteDown, v116PostgresDown))
}
