package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v103SQLiteUp = `
ALTER TABLE gitops_syncs ADD COLUMN pending_redeploy BOOLEAN NOT NULL DEFAULT false;
`
	v103SQLiteDown = `
ALTER TABLE gitops_syncs DROP COLUMN pending_redeploy;
`
	v103PostgresUp = `
ALTER TABLE gitops_syncs ADD COLUMN IF NOT EXISTS pending_redeploy BOOLEAN NOT NULL DEFAULT false;
`
	v103PostgresDown = `
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS pending_redeploy;
`
)

// Migration 103 records GitOps redeploys that started but never finished.
func init() {
	goose.AddMigrationContext(run(v103SQLiteUp, v103PostgresUp), run(v103SQLiteDown, v103PostgresDown))
}
