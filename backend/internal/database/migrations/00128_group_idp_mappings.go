package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v128SQLiteUp = `
CREATE TABLE IF NOT EXISTS group_idp_mappings (
    id TEXT PRIMARY KEY,
    claim_value TEXT NOT NULL,
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'manual',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_gim_claim ON group_idp_mappings(claim_value);
CREATE INDEX IF NOT EXISTS idx_gim_group ON group_idp_mappings(group_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gim_uniq ON group_idp_mappings(claim_value, group_id, source);
`
	v128PostgresUp = `
CREATE TABLE IF NOT EXISTS group_idp_mappings (
    id text PRIMARY KEY,
    claim_value text NOT NULL,
    group_id text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    source text NOT NULL DEFAULT 'manual',
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS idx_gim_claim ON group_idp_mappings USING btree (claim_value);
CREATE INDEX IF NOT EXISTS idx_gim_group ON group_idp_mappings USING btree (group_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gim_uniq ON group_idp_mappings USING btree (claim_value, group_id, source);
`
	v128Down = `
DROP TABLE IF EXISTS group_idp_mappings;
`
)

func init() {
	goose.AddMigrationContext(run(v128SQLiteUp, v128PostgresUp), run(v128Down, v128Down))
}
