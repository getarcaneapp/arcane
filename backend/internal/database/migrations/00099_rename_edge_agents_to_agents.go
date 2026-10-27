package migrations

import (
	"github.com/pressly/goose/v3"
)

// Migration 99 renames edge agents to agents and retires the poll transport.
func init() {
	goose.AddMigrationContext(
		run(v99Up, v99Up),
		run(v99Down, v99Down),
	)
}

const (
	v99Up = `
ALTER TABLE environments RENAME COLUMN last_edge_transport TO last_agent_transport;
UPDATE environments SET last_agent_transport = NULL WHERE last_agent_transport = 'poll';
UPDATE environments SET api_url = 'agent://' || substr(api_url, 8) WHERE api_url LIKE 'edge://%';
`

	v99Down = `
UPDATE environments SET api_url = 'edge://' || substr(api_url, 9) WHERE api_url LIKE 'agent://%';
ALTER TABLE environments RENAME COLUMN last_agent_transport TO last_edge_transport;
`
)
