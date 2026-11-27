package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	// Patch history pruning looks up newer completed records per patched ref.
	v127Up = `
CREATE INDEX IF NOT EXISTS idx_image_patches_env_patched_ref ON image_patches(environment_id, patched_ref, status);
`
	v127Down = `
DROP INDEX IF EXISTS idx_image_patches_env_patched_ref;
`
)

func init() {
	goose.AddMigrationContext(run(v127Up, v127Up), run(v127Down, v127Down))
}
