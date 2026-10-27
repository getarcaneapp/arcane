// Package migrations registers Goose Go migrations during initialization.
// Set Dialect to the target database dialect before running migrations.
package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// BaselineVersion is the final Arcane v2 schema version.
// Older databases must upgrade to Arcane the latest v2 before running v3.
const BaselineVersion int64 = 94

var Dialect goose.Dialect

func run(sqlite, postgres string) goose.GoMigrationContext {
	return func(ctx context.Context, tx *sql.Tx) error {
		statements := sqlite
		if Dialect == goose.DialectPostgres {
			statements = postgres
		}
		_, err := tx.ExecContext(ctx, statements)
		return err
	}
}
