package migrations

import (
	"github.com/pressly/goose/v3"
)

// Migration 96 converts direct environments to edge environments and adds risk score basis columns.
func init() {
	goose.AddMigrationContext(run(v96SQLiteUp, v96PostgresUp), run(v96SQLiteDown, v96PostgresDown))
}

const (
	v96SQLiteUp = `
CREATE TEMP TABLE edge_conversion AS
WITH RECURSIVE walk(id, name, pos, slug) AS (
    SELECT id, lower(coalesce(name, '')), 1, ''
    FROM environments
    WHERE is_edge = 0 AND id <> '0'
    UNION ALL
    SELECT id, name, pos + 1,
        CASE
            WHEN instr('abcdefghijklmnopqrstuvwxyz0123456789', substr(name, pos, 1)) > 0 THEN slug || substr(name, pos, 1)
            WHEN slug = '' OR substr(slug, -1) = '-' THEN slug
            ELSE slug || '-'
        END
    FROM walk
    WHERE pos <= length(name)
)
SELECT id, rtrim(slug, '-') AS slug
FROM walk
WHERE pos = length(name) + 1;

UPDATE environments
SET api_url = 'edge://' || (
        SELECT CASE
            WHEN c.slug = '' THEN c.id
            WHEN EXISTS (
                SELECT 1 FROM environments o
                WHERE o.id NOT IN (SELECT id FROM edge_conversion)
                AND lower(o.api_url) = 'edge://' || c.slug
            ) OR EXISTS (
                SELECT 1 FROM edge_conversion d
                WHERE d.id <> c.id AND d.slug = c.slug
            ) THEN c.slug || '-' || c.id
            ELSE c.slug
        END
        FROM edge_conversion c
        WHERE c.id = environments.id
    ),
    status = 'pending',
    access_token = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM edge_conversion);

DROP TABLE edge_conversion;

ALTER TABLE environments DROP COLUMN is_edge;
ALTER TABLE vulnerability_risk_snapshots ADD COLUMN score_average REAL;
ALTER TABLE vulnerability_risk_snapshots ADD COLUMN score_weight REAL;
ALTER TABLE vulnerability_risk_snapshots ADD COLUMN exploited_running BOOLEAN;
`

	v96SQLiteDown = `
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN exploited_running;
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN score_weight;
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN score_average;
ALTER TABLE environments ADD COLUMN is_edge INTEGER NOT NULL DEFAULT 1;
UPDATE environments SET is_edge = 0 WHERE id = '0';
`

	v96PostgresUp = `
WITH conversion AS (
    SELECT id, trim(both '-' from regexp_replace(lower(coalesce(name, '')), '[^a-z0-9]+', '-', 'g')) AS slug
    FROM environments
    WHERE is_edge = false AND id <> '0'
)
UPDATE environments e
SET api_url = 'edge://' || CASE
        WHEN c.slug = '' THEN c.id
        WHEN EXISTS (
            SELECT 1 FROM environments o
            WHERE o.id NOT IN (SELECT id FROM conversion)
            AND lower(o.api_url) = 'edge://' || c.slug
        ) OR EXISTS (
            SELECT 1 FROM conversion d
            WHERE d.id <> c.id AND d.slug = c.slug
        ) THEN c.slug || '-' || c.id
        ELSE c.slug
    END,
    status = 'pending',
    access_token = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM conversion c
WHERE e.id = c.id;

ALTER TABLE environments DROP COLUMN IF EXISTS is_edge;
ALTER TABLE vulnerability_risk_snapshots ADD COLUMN IF NOT EXISTS score_average DOUBLE PRECISION;
ALTER TABLE vulnerability_risk_snapshots ADD COLUMN IF NOT EXISTS score_weight DOUBLE PRECISION;
ALTER TABLE vulnerability_risk_snapshots ADD COLUMN IF NOT EXISTS exploited_running BOOLEAN;
`

	v96PostgresDown = `
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN IF EXISTS exploited_running;
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN IF EXISTS score_weight;
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN IF EXISTS score_average;
ALTER TABLE environments ADD COLUMN IF NOT EXISTS is_edge BOOLEAN NOT NULL DEFAULT true;
UPDATE environments SET is_edge = false WHERE id = '0';
`
)
