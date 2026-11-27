package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v127SQLiteUp = `
CREATE TABLE IF NOT EXISTS backup_repository_stats (
    id TEXT PRIMARY KEY,
    snapshot_count INTEGER NOT NULL DEFAULT 0,
    stored_bytes INTEGER NOT NULL DEFAULT 0,
    raw_bytes INTEGER NOT NULL DEFAULT 0,
    logical_bytes INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
`
	v127PostgresUp = `
CREATE TABLE IF NOT EXISTS backup_repository_stats (
    id text PRIMARY KEY,
    snapshot_count bigint NOT NULL DEFAULT 0,
    stored_bytes bigint NOT NULL DEFAULT 0,
    raw_bytes bigint NOT NULL DEFAULT 0,
    logical_bytes bigint NOT NULL DEFAULT 0,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone
);
`
	v127Down = `
DROP TABLE IF EXISTS backup_repository_stats;
`
)

func init() {
	goose.AddMigrationContext(run(v127SQLiteUp, v127PostgresUp), run(v127Down, v127Down))
}
