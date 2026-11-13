package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v114SQLiteUp = `
ALTER TABLE image_updates ADD COLUMN blocked_reason TEXT;
ALTER TABLE image_updates ADD COLUMN blocked_policy TEXT;
`
	v114SQLiteDown = `
ALTER TABLE image_updates DROP COLUMN blocked_policy;
ALTER TABLE image_updates DROP COLUMN blocked_reason;
`
	v114PostgresUp = `
ALTER TABLE image_updates ADD COLUMN IF NOT EXISTS blocked_reason text;
ALTER TABLE image_updates ADD COLUMN IF NOT EXISTS blocked_policy text;
`
	v114PostgresDown = `
ALTER TABLE image_updates DROP COLUMN IF EXISTS blocked_policy;
ALTER TABLE image_updates DROP COLUMN IF EXISTS blocked_reason;
`
)

func init() {
	goose.AddMigrationContext(run(v114SQLiteUp, v114PostgresUp), run(v114SQLiteDown, v114PostgresDown))
}
