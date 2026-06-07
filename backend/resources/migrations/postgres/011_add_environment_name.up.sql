ALTER TABLE environments ADD COLUMN IF NOT EXISTS name TEXT;
CREATE INDEX IF NOT EXISTS idx_environments_name ON environments (name);