-- +goose Up
ALTER TABLE environments ADD COLUMN IF NOT EXISTS name TEXT;
CREATE INDEX IF NOT EXISTS idx_environments_name ON environments (name);

-- +goose Down
ALTER TABLE environments DROP COLUMN IF EXISTS name;
DROP INDEX IF EXISTS idx_environments_name;
