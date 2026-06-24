-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_data BYTEA;
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_mime_type TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS avatar_data;
ALTER TABLE users DROP COLUMN IF EXISTS avatar_mime_type;
