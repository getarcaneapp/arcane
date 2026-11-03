package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v103Up   = `ALTER TABLE environments ADD COLUMN swarm_enrollment_token_hash TEXT;`
	v103Down = `ALTER TABLE environments DROP COLUMN swarm_enrollment_token_hash;`
)

func init() {
	goose.AddMigrationContext(
		run(v103Up, v103Up),
		run(v103Down, v103Down),
	)
}
