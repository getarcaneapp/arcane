-- Remove auto_update_cron column
ALTER TABLE projects DROP COLUMN auto_update_cron;

-- Restore auto_update as NOT NULL with default
ALTER TABLE projects DROP COLUMN auto_update;
ALTER TABLE projects ADD COLUMN auto_update BOOLEAN NOT NULL DEFAULT false;

