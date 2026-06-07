ALTER TABLE environments DROP COLUMN IF EXISTS access_token;
ALTER TABLE environments ADD COLUMN IF NOT EXISTS hostname TEXT NOT NULL DEFAULT '';
ALTER TABLE environments ADD COLUMN IF NOT EXISTS description TEXT;
DROP INDEX IF EXISTS idx_environments_api_url;