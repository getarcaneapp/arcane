package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v117SQLiteUp = `
ALTER TABLE environments ADD COLUMN docker_host TEXT;
`
	v117SQLiteDown = `
ALTER TABLE environments DROP COLUMN docker_host;
`
	v117PostgresUp = `
ALTER TABLE environments ADD COLUMN IF NOT EXISTS docker_host text;
`
	v117PostgresDown = `
ALTER TABLE environments DROP COLUMN IF EXISTS docker_host;
`
)

func init() {
	goose.AddMigrationContext(run(v117SQLiteUp, v117PostgresUp), run(v117SQLiteDown, v117PostgresDown))
}
