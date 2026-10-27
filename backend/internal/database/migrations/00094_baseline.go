package migrations

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"

	"github.com/pressly/goose/v3"
)

// Migration 94 creates the schema of a database migrated through Arcane v2.17.1.
func init() {
	goose.AddMigrationContext(run(baselineSQLite, baselinePostgres), down94)
}

var (
	//go:embed baseline/sqlite.sql
	baselineSQLite string
	//go:embed baseline/postgres.sql
	baselinePostgres string
)

func down94(context.Context, *sql.Tx) error {
	return errors.New("cannot downgrade below the Arcane v2.17.1 schema baseline; restore the database from a backup")
}
