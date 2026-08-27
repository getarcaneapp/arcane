-- +goose Up
-- Add image_patches table for Copacetic image patch history
CREATE TABLE IF NOT EXISTS image_patches (
    id TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL,
    original_image_id TEXT NOT NULL,
    original_ref TEXT NOT NULL,
    original_digest TEXT,
    patched_ref TEXT NOT NULL,
    mode TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'patching',
    packages_updated INTEGER,
    error TEXT,
    activity_id TEXT,
    duration_ms INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_image_patches_env_image ON image_patches(environment_id, original_image_id);
CREATE INDEX IF NOT EXISTS idx_image_patches_created_at ON image_patches(created_at);

-- Store the raw Trivy report path and fixable-vulnerability count on scans so
-- report-based patching can consume them.
ALTER TABLE vulnerability_scans ADD COLUMN report_path TEXT;
ALTER TABLE vulnerability_scans ADD COLUMN fixable_count INTEGER;

-- +goose Down
ALTER TABLE vulnerability_scans DROP COLUMN fixable_count;
ALTER TABLE vulnerability_scans DROP COLUMN report_path;
DROP TABLE IF EXISTS image_patches;
