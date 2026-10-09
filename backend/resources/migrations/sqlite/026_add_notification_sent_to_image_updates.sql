-- +goose Up
-- Add notification_sent column to track if updates have been sent
-- Existing indexes on repository and tag are preserved automatically
ALTER TABLE image_updates ADD COLUMN notification_sent INTEGER DEFAULT 0;

-- +goose Down
ALTER TABLE image_updates DROP COLUMN notification_sent;
