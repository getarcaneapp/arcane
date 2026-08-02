-- +goose Up
ALTER TABLE volume_backups ADD COLUMN destination TEXT NOT NULL DEFAULT 'local';
UPDATE volume_backups SET destination = 'local_s3' WHERE COALESCE(remote_key, '') <> '';

-- +goose Down
ALTER TABLE volume_backups DROP COLUMN destination;
