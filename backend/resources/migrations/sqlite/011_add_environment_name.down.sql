ALTER TABLE environments DROP COLUMN name;
DROP INDEX IF EXISTS idx_environments_name;