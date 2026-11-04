package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v104SQLiteUp = `
CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,
    token_encrypted TEXT NOT NULL,
    environment_id  TEXT,
    max_uses        INTEGER,
    use_count       INTEGER NOT NULL DEFAULT 0,
    last_used_at    DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_enrollment_tokens_environment_id ON enrollment_tokens(environment_id) WHERE environment_id IS NOT NULL;
`
	v104PostgresUp = `
CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id text PRIMARY KEY,
    name text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    token_encrypted text NOT NULL,
    environment_id text,
    max_uses integer,
    use_count integer DEFAULT 0 NOT NULL,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_enrollment_tokens_environment_id ON enrollment_tokens(environment_id) WHERE environment_id IS NOT NULL;
`
	v104Down = `
DROP TABLE IF EXISTS enrollment_tokens;
`
)

func init() {
	goose.AddMigrationContext(
		run(v104SQLiteUp, v104PostgresUp),
		run(v104Down, v104Down),
	)
}
