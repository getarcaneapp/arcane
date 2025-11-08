-- Drop and recreate auto_update as nullable (NULL = follow global setting)
ALTER TABLE projects DROP COLUMN auto_update;
ALTER TABLE projects ADD COLUMN auto_update BOOLEAN;

-- Add auto_update_cron column (NULL = immediate)
ALTER TABLE projects ADD COLUMN auto_update_cron TEXT;

