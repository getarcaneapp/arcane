ALTER TABLE environments DROP COLUMN access_token;
ALTER TABLE environments ADD COLUMN hostname TEXT NOT NULL DEFAULT '';
ALTER TABLE environments ADD COLUMN description TEXT;
DROP INDEX IF EXISTS idx_environments_api_url;