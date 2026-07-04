-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS navigation_layout TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS navigation_layout;
