package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v106SQLiteUp = `
ALTER TABLE image_updates ADD COLUMN current_created DATETIME;
ALTER TABLE image_updates ADD COLUMN latest_created DATETIME;
`
	v106SQLiteDown = `
ALTER TABLE image_updates DROP COLUMN latest_created;
ALTER TABLE image_updates DROP COLUMN current_created;
`
	v106PostgresUp = `
ALTER TABLE image_updates ADD COLUMN IF NOT EXISTS current_created timestamp with time zone;
ALTER TABLE image_updates ADD COLUMN IF NOT EXISTS latest_created timestamp with time zone;
`
	v106PostgresDown = `
ALTER TABLE image_updates DROP COLUMN IF EXISTS latest_created;
ALTER TABLE image_updates DROP COLUMN IF EXISTS current_created;
`
)

func init() {
	goose.AddMigrationContext(run(v106SQLiteUp, v106PostgresUp), run(v106SQLiteDown, v106PostgresDown))
}
