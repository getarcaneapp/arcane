package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v105Up = `
ALTER TABLE global_variables ADD COLUMN inject_into_containers BOOLEAN NOT NULL DEFAULT false;
UPDATE global_variables SET inject_into_containers = true;
`
	v105Down = `
ALTER TABLE global_variables DROP COLUMN inject_into_containers;
`
)

func init() {
	goose.AddMigrationContext(run(v105Up, v105Up), run(v105Down, v105Down))
}
