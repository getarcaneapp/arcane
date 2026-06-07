-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS font_size INTEGER;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS font_size;
