-- +goose Up
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'scoped';

-- +goose Down
ALTER TABLE api_keys DROP COLUMN IF EXISTS kind;
