-- Revert image_ref back to VARCHAR(255)
ALTER TABLE notification_logs ALTER COLUMN image_ref TYPE VARCHAR(255);
