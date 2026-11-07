package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	// Installs that predate unmanaged backup cleanup keep their backups until an admin opts in.
	v108KeepExistingBackups = `
INSERT INTO settings (key, value)
SELECT 'unmanagedBackupRetentionDays', '0'
WHERE EXISTS (SELECT 1 FROM settings WHERE key = 'instanceId')
  AND NOT EXISTS (SELECT 1 FROM settings WHERE key = 'unmanagedBackupRetentionDays');
`
	v108SQLiteUp = `
ALTER TABLE volume_backups ADD COLUMN orphaned_at DATETIME;
ALTER TABLE system_backup_runs ADD COLUMN orphaned_at DATETIME;
` + v108KeepExistingBackups
	v108PostgresUp = `
ALTER TABLE volume_backups ADD COLUMN IF NOT EXISTS orphaned_at timestamp with time zone;
ALTER TABLE system_backup_runs ADD COLUMN IF NOT EXISTS orphaned_at timestamp with time zone;
` + v108KeepExistingBackups
	v108SQLiteDown = `
DELETE FROM settings WHERE key = 'unmanagedBackupRetentionDays';
ALTER TABLE system_backup_runs DROP COLUMN orphaned_at;
ALTER TABLE volume_backups DROP COLUMN orphaned_at;
`
	v108PostgresDown = `
DELETE FROM settings WHERE key = 'unmanagedBackupRetentionDays';
ALTER TABLE system_backup_runs DROP COLUMN IF EXISTS orphaned_at;
ALTER TABLE volume_backups DROP COLUMN IF EXISTS orphaned_at;
`
)

func init() {
	goose.AddMigrationContext(run(v108SQLiteUp, v108PostgresUp), run(v108SQLiteDown, v108PostgresDown))
}
