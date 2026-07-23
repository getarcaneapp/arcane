-- +goose Up
ALTER TABLE users ADD COLUMN table_prefs JSONB;

-- +goose Down
ALTER TABLE users DROP COLUMN table_prefs;
