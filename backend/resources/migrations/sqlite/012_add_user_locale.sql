-- +goose Up
ALTER TABLE users ADD COLUMN locale TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN locale;
