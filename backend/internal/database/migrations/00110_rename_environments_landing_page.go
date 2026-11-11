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
UPDATE users SET preferences = jsonb_set(preferences::jsonb, '{defaultLandingPage}', '"/agents"')::text
WHERE CASE WHEN preferences LIKE '%"/environments"%' THEN preferences::jsonb->>'defaultLandingPage' = '/environments' ELSE false END;
`
	v110SQLiteDown = `
UPDATE users SET preferences = json_set(preferences, '$.defaultLandingPage', '/environments')
WHERE CASE WHEN json_valid(preferences) THEN json_extract(preferences, '$.defaultLandingPage') = '/agents' ELSE 0 END;
`
	v110PostgresDown = `
UPDATE users SET preferences = jsonb_set(preferences::jsonb, '{defaultLandingPage}', '"/environments"')::text
WHERE CASE WHEN preferences LIKE '%"/agents"%' THEN preferences::jsonb->>'defaultLandingPage' = '/agents' ELSE false END;
`
)

func init() {
	goose.AddMigrationContext(run(v110SQLiteUp, v110PostgresUp), run(v110SQLiteDown, v110PostgresDown))
}
