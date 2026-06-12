-- +goose Up
-- Add notification_sent column to track if updates have been sent
-- Existing indexes on repository and tag are preserved automatically
ALTER TABLE image_updates ADD COLUMN notification_sent INTEGER DEFAULT 0;

-- +goose Down
-- SQLite doesn't support DROP COLUMN directly, so we need to recreate the table
-- Create backup without notification_sent column
CREATE TABLE image_updates_backup AS SELECT 
    id, repository, tag, has_update, update_type, current_version, 
    latest_version, current_digest, latest_digest, check_time, 
    response_time_ms, last_error, auth_method, auth_username, 
    auth_registry, used_credential, created_at, updated_at
FROM image_updates;

DROP TABLE image_updates;

ALTER TABLE image_updates_backup RENAME TO image_updates;

-- Recreate indexes that were lost during table recreation
CREATE INDEX IF NOT EXISTS idx_image_update_repository ON image_updates(repository);
CREATE INDEX IF NOT EXISTS idx_image_update_tag ON image_updates(tag);
