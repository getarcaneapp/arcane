-- +goose Up
ALTER TABLE environments ADD COLUMN name TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_environments_name ON environments(name);

-- +goose Down
DROP INDEX IF EXISTS idx_environments_name;
ALTER TABLE environments DROP COLUMN name;
