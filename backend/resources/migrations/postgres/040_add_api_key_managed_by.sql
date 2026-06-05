-- +goose Up
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS managed_by TEXT;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN IF EXISTS managed_by;
