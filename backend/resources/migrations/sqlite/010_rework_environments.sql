-- +goose Up
ALTER TABLE environments DROP COLUMN hostname;
ALTER TABLE environments DROP COLUMN description;
ALTER TABLE environments ADD COLUMN access_token TEXT;
CREATE INDEX IF NOT EXISTS idx_environments_api_url ON environments(api_url);

-- +goose Down
ALTER TABLE environments DROP COLUMN access_token;
ALTER TABLE environments ADD COLUMN hostname TEXT NOT NULL DEFAULT '';
ALTER TABLE environments ADD COLUMN description TEXT;
DROP INDEX IF EXISTS idx_environments_api_url;
