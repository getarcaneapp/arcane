-- +goose Up
ALTER TABLE volume_backups ADD COLUMN remote_instance_id text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE volume_backups DROP COLUMN remote_instance_id;
