-- +goose Up
ALTER TABLE users DROP COLUMN IF EXISTS require_password_change;

-- +goose Down
ALTER TABLE users ADD COLUMN IF NOT EXISTS require_password_change BOOLEAN NOT NULL DEFAULT false;
