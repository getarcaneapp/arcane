-- +goose Up
ALTER TABLE users ADD COLUMN font_size INTEGER;

-- +goose Down
ALTER TABLE users DROP COLUMN font_size;
