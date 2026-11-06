package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v107SQLiteUp = `
ALTER TABLE users ADD COLUMN backdrop_hash TEXT;
CREATE TABLE user_backdrops (
    user_id TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    mime_type TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
`
	v107PostgresUp = `
ALTER TABLE users ADD COLUMN IF NOT EXISTS backdrop_hash text;
CREATE TABLE IF NOT EXISTS user_backdrops (
    user_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    data bytea NOT NULL,
    mime_type text NOT NULL
);
`
	v107SQLiteDown = `
DROP TABLE user_backdrops;
ALTER TABLE users DROP COLUMN backdrop_hash;
`
	v107PostgresDown = `
DROP TABLE IF EXISTS user_backdrops;
ALTER TABLE users DROP COLUMN IF EXISTS backdrop_hash;
`
)

func init() {
	goose.AddMigrationContext(run(v107SQLiteUp, v107PostgresUp), run(v107SQLiteDown, v107PostgresDown))
}
