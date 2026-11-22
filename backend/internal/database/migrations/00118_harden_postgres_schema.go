package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v118SQLiteUp = `
DROP INDEX IF EXISTS idx_api_keys_key_hash;
CREATE UNIQUE INDEX idx_api_keys_key_hash ON api_keys (key_hash);
`
	v118PostgresUp = `
DROP INDEX IF EXISTS idx_api_keys_key_hash;
CREATE UNIQUE INDEX idx_api_keys_key_hash ON api_keys (key_hash);

ALTER TABLE image_builds
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN completed_at TYPE timestamp with time zone USING completed_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE image_patches
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE notification_settings
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN updated_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP,
    ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE projects
    ALTER COLUMN archived_at TYPE timestamp with time zone USING archived_at AT TIME ZONE 'UTC';

ALTER TABLE user_sessions
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN last_used_at TYPE timestamp with time zone USING last_used_at AT TIME ZONE 'UTC',
    ALTER COLUMN expires_at TYPE timestamp with time zone USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN revoked_at TYPE timestamp with time zone USING revoked_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE vulnerability_ignores
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE vulnerability_reports
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE vulnerability_scans
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN scan_time TYPE timestamp with time zone USING scan_time AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE activities ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE activity_messages ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE custom_images ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE environment_update_jobs ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE global_variables ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE volume_backups ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
`
	v118SQLiteDown = `
DROP INDEX IF EXISTS idx_api_keys_key_hash;
CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash);
`
	v118PostgresDown = `
DROP INDEX IF EXISTS idx_api_keys_key_hash;
CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash);

ALTER TABLE activities ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE activity_messages ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE custom_images ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE environment_update_jobs ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE global_variables ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE volume_backups ALTER COLUMN created_at DROP DEFAULT;

ALTER TABLE image_builds
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN completed_at TYPE timestamp without time zone USING completed_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp without time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp without time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE image_patches
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp without time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp without time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE notification_settings
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN updated_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp without time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp without time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP,
    ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE projects
    ALTER COLUMN archived_at TYPE timestamp without time zone USING archived_at AT TIME ZONE 'UTC';

ALTER TABLE user_sessions
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp without time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp without time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN last_used_at TYPE timestamp without time zone USING last_used_at AT TIME ZONE 'UTC',
    ALTER COLUMN expires_at TYPE timestamp without time zone USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN revoked_at TYPE timestamp without time zone USING revoked_at AT TIME ZONE 'UTC';

ALTER TABLE vulnerability_ignores
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp without time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE vulnerability_reports
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at TYPE timestamp without time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp without time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE vulnerability_scans
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN scan_time TYPE timestamp without time zone USING scan_time AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp without time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp without time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
`
)

func init() {
	goose.AddMigrationContext(run(v118SQLiteUp, v118PostgresUp), run(v118SQLiteDown, v118PostgresDown))
}
