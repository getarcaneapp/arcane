package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v122SQLiteUp = `
ALTER TABLE image_updates ADD COLUMN environment_id TEXT NOT NULL DEFAULT '0';
UPDATE image_updates SET environment_id = '0' WHERE environment_id = '';
CREATE INDEX IF NOT EXISTS idx_image_updates_environment_id ON image_updates(environment_id);
`
	v122SQLiteDown = `
DROP INDEX IF EXISTS idx_image_updates_environment_id;
ALTER TABLE image_updates DROP COLUMN environment_id;
`
	v122PostgresUp = `
ALTER TABLE image_updates ADD COLUMN IF NOT EXISTS environment_id text NOT NULL DEFAULT '0';
UPDATE image_updates SET environment_id = '0' WHERE environment_id = '';
CREATE INDEX IF NOT EXISTS idx_image_updates_environment_id ON image_updates(environment_id);
`
	v122PostgresDown = `
DROP INDEX IF EXISTS idx_image_updates_environment_id;
ALTER TABLE image_updates DROP COLUMN IF EXISTS environment_id;
`
)

func init() {
	goose.AddMigrationContext(run(v122SQLiteUp, v122PostgresUp), run(v122SQLiteDown, v122PostgresDown))
}
