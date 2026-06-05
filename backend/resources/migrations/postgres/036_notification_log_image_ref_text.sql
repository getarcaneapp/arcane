-- +goose Up
-- Change image_ref from VARCHAR(255) to TEXT to support batch notifications with multiple image refs
ALTER TABLE notification_logs ALTER COLUMN image_ref TYPE TEXT;

-- +goose Down
-- Revert image_ref back to VARCHAR(255)
ALTER TABLE notification_logs ALTER COLUMN image_ref TYPE VARCHAR(255);
