-- +goose Up
ALTER TABLE IF EXISTS image_updates
    ADD COLUMN IF NOT EXISTS auth_method TEXT,
    ADD COLUMN IF NOT EXISTS auth_username TEXT,
    ADD COLUMN IF NOT EXISTS auth_registry TEXT,
    ADD COLUMN IF NOT EXISTS used_credential BOOLEAN DEFAULT FALSE;

-- +goose Down
ALTER TABLE IF EXISTS image_updates
    DROP COLUMN IF EXISTS used_credential,
    DROP COLUMN IF EXISTS auth_registry,
    DROP COLUMN IF EXISTS auth_username,
    DROP COLUMN IF EXISTS auth_method;
