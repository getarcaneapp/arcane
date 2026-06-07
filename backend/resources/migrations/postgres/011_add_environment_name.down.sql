ALTER TABLE environments DROP COLUMN IF EXISTS name;
DROP INDEX IF EXISTS idx_environments_name;