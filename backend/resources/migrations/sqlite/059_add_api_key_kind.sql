-- +goose Up
ALTER TABLE api_keys ADD COLUMN kind TEXT NOT NULL DEFAULT 'scoped';

-- +goose Down
ALTER TABLE api_keys DROP COLUMN kind;
