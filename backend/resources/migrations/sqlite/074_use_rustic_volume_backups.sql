-- +goose Up
DELETE FROM volume_backups;
ALTER TABLE volume_backups ADD COLUMN local_snapshot_id TEXT;
ALTER TABLE volume_backups ADD COLUMN remote_snapshot_id TEXT;

-- +goose Down
DELETE FROM volume_backups;
ALTER TABLE volume_backups DROP COLUMN remote_snapshot_id;
ALTER TABLE volume_backups DROP COLUMN local_snapshot_id;
