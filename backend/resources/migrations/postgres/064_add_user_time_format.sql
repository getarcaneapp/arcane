-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS time_format TEXT NOT NULL DEFAULT 'auto'
    CHECK (time_format IN ('auto', '12h', '24h'));

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS time_format;
