-- +goose Up
ALTER TABLE users DROP COLUMN require_password_change;

-- +goose Down
ALTER TABLE users ADD COLUMN require_password_change BOOLEAN NOT NULL DEFAULT 0;
