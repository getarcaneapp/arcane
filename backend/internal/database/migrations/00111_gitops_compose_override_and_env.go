package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v111SQLiteUp = `
ALTER TABLE gitops_syncs ADD COLUMN compose_override_path TEXT NOT NULL DEFAULT '';
ALTER TABLE gitops_syncs ADD COLUMN env_content TEXT;
`
	v111PostgresUp = `
ALTER TABLE gitops_syncs ADD COLUMN IF NOT EXISTS compose_override_path text NOT NULL DEFAULT '';
ALTER TABLE gitops_syncs ADD COLUMN IF NOT EXISTS env_content text;
`
	v111SQLiteDown = `
ALTER TABLE gitops_syncs DROP COLUMN env_content;
ALTER TABLE gitops_syncs DROP COLUMN compose_override_path;
`
	v111PostgresDown = `
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS env_content;
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS compose_override_path;
`
)

func init() {
	goose.AddMigrationContext(run(v111SQLiteUp, v111PostgresUp), run(v111SQLiteDown, v111PostgresDown))
}
