-- Remove notification_sent column
-- PostgreSQL automatically preserves existing indexes on other columns
ALTER TABLE IF EXISTS image_updates
    DROP COLUMN notification_sent;
