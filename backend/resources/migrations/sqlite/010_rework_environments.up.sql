ALTER TABLE environments DROP COLUMN hostname;
ALTER TABLE environments DROP COLUMN description;
ALTER TABLE environments ADD COLUMN access_token TEXT;
CREATE INDEX IF NOT EXISTS idx_environments_api_url ON environments(api_url);