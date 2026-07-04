-- +goose Up
ALTER TABLE users ADD COLUMN navigation_layout TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN navigation_layout;
