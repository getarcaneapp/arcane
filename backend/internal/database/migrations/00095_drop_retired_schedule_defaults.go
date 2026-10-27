package migrations

import (
	"github.com/pressly/goose/v3"
)

// Migration 95 drops retired schedule defaults. The current defaults apply without them, so down restores nothing.
func init() {
	goose.AddMigrationContext(run(v95Up, v95Up), nil)
}

const (
	v95Up = `
DELETE FROM settings WHERE key IN ('dockerClientRefreshInterval', 'autoHealInterval') AND value = '*/30 * * * * *';
`
)
