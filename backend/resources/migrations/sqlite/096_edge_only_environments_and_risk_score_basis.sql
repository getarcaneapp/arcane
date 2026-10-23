-- +goose Up
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

-- +goose Down
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN exploited_running;
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN score_weight;
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN score_average;
ALTER TABLE environments ADD COLUMN is_edge INTEGER NOT NULL DEFAULT 1;
UPDATE environments SET is_edge = 0 WHERE id = '0';
