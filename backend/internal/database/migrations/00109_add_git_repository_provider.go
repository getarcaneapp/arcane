package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v109SQLiteUp = `
ALTER TABLE git_repositories ADD COLUMN provider TEXT NOT NULL DEFAULT '';
`
	v109PostgresUp = `
ALTER TABLE git_repositories ADD COLUMN IF NOT EXISTS provider text NOT NULL DEFAULT '';
`
	v109SQLiteDown = `
ALTER TABLE git_repositories DROP COLUMN provider;
`
	v109PostgresDown = `
ALTER TABLE git_repositories DROP COLUMN IF EXISTS provider;
`
)

func init() {
	goose.AddMigrationContext(run(v109SQLiteUp, v109PostgresUp), run(v109SQLiteDown, v109PostgresDown))
}
