ALTER TABLE environments ADD COLUMN name TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_environments_name ON environments(name);