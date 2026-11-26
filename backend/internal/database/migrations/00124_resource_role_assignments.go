package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v124SQLiteUp = `
ALTER TABLE user_role_assignments ADD COLUMN resource_type TEXT;
ALTER TABLE user_role_assignments ADD COLUMN resource_id TEXT;
DROP INDEX IF EXISTS idx_ura_uniq;
CREATE UNIQUE INDEX idx_ura_uniq ON user_role_assignments(user_id, role_id, COALESCE(environment_id, ''), COALESCE(resource_type, ''), COALESCE(resource_id, ''), source);
CREATE INDEX IF NOT EXISTS idx_ura_resource ON user_role_assignments(environment_id, resource_type, resource_id);

ALTER TABLE group_role_assignments ADD COLUMN resource_type TEXT;
ALTER TABLE group_role_assignments ADD COLUMN resource_id TEXT;
DROP INDEX IF EXISTS idx_gra_uniq;
CREATE UNIQUE INDEX idx_gra_uniq ON group_role_assignments(group_id, role_id, COALESCE(environment_id, ''), COALESCE(resource_type, ''), COALESCE(resource_id, ''), source);
CREATE INDEX IF NOT EXISTS idx_gra_resource ON group_role_assignments(environment_id, resource_type, resource_id);
`
	v124PostgresUp = `
ALTER TABLE user_role_assignments ADD COLUMN IF NOT EXISTS resource_type text;
ALTER TABLE user_role_assignments ADD COLUMN IF NOT EXISTS resource_id text;
DROP INDEX IF EXISTS idx_ura_uniq;
CREATE UNIQUE INDEX idx_ura_uniq ON user_role_assignments USING btree (
    user_id, role_id, COALESCE(environment_id, ''::text), COALESCE(resource_type, ''::text), COALESCE(resource_id, ''::text), source
);
CREATE INDEX IF NOT EXISTS idx_ura_resource ON user_role_assignments USING btree (environment_id, resource_type, resource_id);

ALTER TABLE group_role_assignments ADD COLUMN IF NOT EXISTS resource_type text;
ALTER TABLE group_role_assignments ADD COLUMN IF NOT EXISTS resource_id text;
DROP INDEX IF EXISTS idx_gra_uniq;
CREATE UNIQUE INDEX idx_gra_uniq ON group_role_assignments USING btree (
    group_id, role_id, COALESCE(environment_id, ''::text), COALESCE(resource_type, ''::text), COALESCE(resource_id, ''::text), source
);
CREATE INDEX IF NOT EXISTS idx_gra_resource ON group_role_assignments USING btree (environment_id, resource_type, resource_id);
`
	v124SQLiteDown = `
DELETE FROM user_role_assignments WHERE resource_type IS NOT NULL;
DROP INDEX IF EXISTS idx_ura_resource;
DROP INDEX IF EXISTS idx_ura_uniq;
ALTER TABLE user_role_assignments DROP COLUMN resource_id;
ALTER TABLE user_role_assignments DROP COLUMN resource_type;
CREATE UNIQUE INDEX idx_ura_uniq ON user_role_assignments(user_id, role_id, COALESCE(environment_id, ''), source);

DELETE FROM group_role_assignments WHERE resource_type IS NOT NULL;
DROP INDEX IF EXISTS idx_gra_resource;
DROP INDEX IF EXISTS idx_gra_uniq;
ALTER TABLE group_role_assignments DROP COLUMN resource_id;
ALTER TABLE group_role_assignments DROP COLUMN resource_type;
CREATE UNIQUE INDEX idx_gra_uniq ON group_role_assignments(group_id, role_id, COALESCE(environment_id, ''), source);
`
	v124PostgresDown = `
DELETE FROM user_role_assignments WHERE resource_type IS NOT NULL;
DROP INDEX IF EXISTS idx_ura_resource;
DROP INDEX IF EXISTS idx_ura_uniq;
ALTER TABLE user_role_assignments DROP COLUMN IF EXISTS resource_id;
ALTER TABLE user_role_assignments DROP COLUMN IF EXISTS resource_type;
CREATE UNIQUE INDEX idx_ura_uniq ON user_role_assignments USING btree (user_id, role_id, COALESCE(environment_id, ''::text), source);

DELETE FROM group_role_assignments WHERE resource_type IS NOT NULL;
DROP INDEX IF EXISTS idx_gra_resource;
DROP INDEX IF EXISTS idx_gra_uniq;
ALTER TABLE group_role_assignments DROP COLUMN IF EXISTS resource_id;
ALTER TABLE group_role_assignments DROP COLUMN IF EXISTS resource_type;
CREATE UNIQUE INDEX idx_gra_uniq ON group_role_assignments USING btree (group_id, role_id, COALESCE(environment_id, ''::text), source);
`
)

func init() {
	goose.AddMigrationContext(run(v124SQLiteUp, v124PostgresUp), run(v124SQLiteDown, v124PostgresDown))
}
