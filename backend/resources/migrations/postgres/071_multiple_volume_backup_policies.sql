-- +goose Up
ALTER TABLE volume_backups ADD COLUMN policy_id TEXT;
CREATE INDEX idx_volume_backups_policy_id ON volume_backups(policy_id);
ALTER TABLE volume_backup_policies DROP CONSTRAINT volume_backup_policies_volume_name_key;
CREATE INDEX idx_volume_backup_policies_volume_name ON volume_backup_policies(volume_name);

-- +goose Down
DELETE FROM volume_backup_policies a
USING volume_backup_policies b
WHERE a.volume_name = b.volume_name AND a.id > b.id;
DROP INDEX idx_volume_backup_policies_volume_name;
ALTER TABLE volume_backup_policies ADD CONSTRAINT volume_backup_policies_volume_name_key UNIQUE (volume_name);
DROP INDEX idx_volume_backups_policy_id;
ALTER TABLE volume_backups DROP COLUMN policy_id;
