package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v102Up = `
ALTER TABLE global_variables ADD COLUMN inject_into_containers BOOLEAN NOT NULL DEFAULT false;
UPDATE global_variables SET inject_into_containers = true;
`
	v102Down = `
ALTER TABLE global_variables DROP COLUMN inject_into_containers;
`
)

// Migration 102 makes new global variables interpolation-only by default while
// existing ones, which were always injected into every container, keep it.
func init() {
	goose.AddMigrationContext(run(v102Up, v102Up), run(v102Down, v102Down))
}
