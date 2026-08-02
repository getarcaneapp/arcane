-- +goose Up
ALTER TABLE volume_backup_policies ADD COLUMN stop_containers INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE volume_backup_policies DROP COLUMN stop_containers;
