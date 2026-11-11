package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v110SQLiteUp = `
UPDATE users SET preferences = json_set(preferences, '$.defaultLandingPage', '/agents')
WHERE CASE WHEN json_valid(preferences) THEN json_extract(preferences, '$.defaultLandingPage') = '/environments' ELSE 0 END;
`
	v110PostgresUp = `
UPDATE users SET preferences = regexp_replace(preferences, '([{,]\s*)"defaultLandingPage"\s*:\s*"/environments"', '\1"defaultLandingPage":"/agents"')
WHERE preferences ~ '[{,]\s*"defaultLandingPage"\s*:\s*"/environments"';
`
	v110SQLiteDown = `
UPDATE users SET preferences = json_set(preferences, '$.defaultLandingPage', '/environments')
WHERE CASE WHEN json_valid(preferences) THEN json_extract(preferences, '$.defaultLandingPage') = '/agents' ELSE 0 END;
`
	v110PostgresDown = `
UPDATE users SET preferences = regexp_replace(preferences, '([{,]\s*)"defaultLandingPage"\s*:\s*"/agents"', '\1"defaultLandingPage":"/environments"')
WHERE preferences ~ '[{,]\s*"defaultLandingPage"\s*:\s*"/agents"';
`
)

func init() {
	goose.AddMigrationContext(run(v110SQLiteUp, v110PostgresUp), run(v110SQLiteDown, v110PostgresDown))
}
