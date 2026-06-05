-- +goose Up
-- Add notification_sent column to track if updates have been sent
-- Existing indexes on repository and tag are preserved automatically
ALTER TABLE IF EXISTS image_updates
    ADD COLUMN notification_sent BOOLEAN DEFAULT false;

-- +goose Down
-- Remove notification_sent column
-- PostgreSQL automatically preserves existing indexes on other columns
ALTER TABLE IF EXISTS image_updates
    DROP COLUMN notification_sent;
