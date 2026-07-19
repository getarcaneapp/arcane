-- +goose Up
ALTER TABLE volume_backup_policies ADD COLUMN stop_containers BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE volume_backup_policies DROP COLUMN stop_containers;
