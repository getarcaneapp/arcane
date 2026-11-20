package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v118SQLiteUp = `
ALTER TABLE volume_backups ADD COLUMN environment_id TEXT NOT NULL DEFAULT '0';
UPDATE volume_backups SET environment_id = '0' WHERE environment_id = '';
CREATE INDEX IF NOT EXISTS idx_volume_backups_environment_id ON volume_backups(environment_id);

ALTER TABLE volume_backup_policies ADD COLUMN environment_id TEXT NOT NULL DEFAULT '0';
UPDATE volume_backup_policies SET environment_id = '0' WHERE environment_id = '';
CREATE INDEX IF NOT EXISTS idx_volume_backup_policies_environment_id ON volume_backup_policies(environment_id);
`
	v118SQLiteDown = `
DROP INDEX IF EXISTS idx_volume_backup_policies_environment_id;
ALTER TABLE volume_backup_policies DROP COLUMN environment_id;
DROP INDEX IF EXISTS idx_volume_backups_environment_id;
ALTER TABLE volume_backups DROP COLUMN environment_id;
`
	v118PostgresUp = `
ALTER TABLE volume_backups ADD COLUMN IF NOT EXISTS environment_id text NOT NULL DEFAULT '0';
UPDATE volume_backups SET environment_id = '0' WHERE environment_id = '';
CREATE INDEX IF NOT EXISTS idx_volume_backups_environment_id ON volume_backups(environment_id);

ALTER TABLE volume_backup_policies ADD COLUMN IF NOT EXISTS environment_id text NOT NULL DEFAULT '0';
UPDATE volume_backup_policies SET environment_id = '0' WHERE environment_id = '';
CREATE INDEX IF NOT EXISTS idx_volume_backup_policies_environment_id ON volume_backup_policies(environment_id);
`
	v118PostgresDown = `
DROP INDEX IF EXISTS idx_volume_backup_policies_environment_id;
ALTER TABLE volume_backup_policies DROP COLUMN IF EXISTS environment_id;
DROP INDEX IF EXISTS idx_volume_backups_environment_id;
ALTER TABLE volume_backups DROP COLUMN IF EXISTS environment_id;
`
)

func init() {
	goose.AddMigrationContext(run(v118SQLiteUp, v118PostgresUp), run(v118SQLiteDown, v118PostgresDown))
}
