-- +goose Up
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

-- +goose Down
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN IF EXISTS exploited_running;
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN IF EXISTS score_weight;
ALTER TABLE vulnerability_risk_snapshots DROP COLUMN IF EXISTS score_average;
ALTER TABLE environments ADD COLUMN IF NOT EXISTS is_edge BOOLEAN NOT NULL DEFAULT true;
UPDATE environments SET is_edge = false WHERE id = '0';
