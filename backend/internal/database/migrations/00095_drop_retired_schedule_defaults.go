package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v95Up = `
DELETE FROM settings WHERE key IN ('dockerClientRefreshInterval', 'autoHealInterval') AND value = '*/30 * * * * *';
`
)

func init() {
	goose.AddMigrationContext(run(v95Up, v95Up), nil)
}
