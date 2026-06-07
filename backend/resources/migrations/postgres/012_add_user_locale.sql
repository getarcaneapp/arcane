-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS locale TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS locale;
